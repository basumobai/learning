package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestResourceImmutableDedupAndIndependentTags(t *testing.T) {
	s, student, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	pdf, _ := os.ReadFile("testdata/questions.pdf")
	id, err := s.storeRawResource(ctx, "Grammar.pdf", pdf, nil, resourceSource{Kind: "upload", Name: "Grammar.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	twice, err := s.storeRawResource(ctx, "Renamed.pdf", pdf, nil, resourceSource{Kind: "upload", Name: "Renamed.pdf"})
	if err != nil || twice != id {
		t.Fatal("dedupe", twice, id, err)
	}
	var blobs, sources int
	s.db.QueryRow(`SELECT count(*) FROM learning_blobs`).Scan(&blobs)
	s.db.QueryRow(`SELECT count(*) FROM learning_resource_sources WHERE document_id=$1`, id).Scan(&sources)
	if blobs != 1 || sources != 2 {
		t.Fatal(blobs, sources)
	}
	if _, err = s.db.Exec(`UPDATE learning_blobs SET data=data`); err == nil {
		t.Fatal("original updated")
	}
	if _, err = s.db.Exec(`DELETE FROM learning_blobs`); err == nil {
		t.Fatal("original deleted")
	}
	in := resourceTags{Revision: 1, Type: "grammar_practice", Subjects: []string{"english"}, Topics: []string{"定语从句", "非谓语动词"}, Alignment: "no", Confirm: true}
	student.call("PATCH", "/api/v1/admin/resources/"+id+"/tags", in, 403)
	admin.call("PATCH", "/api/v1/admin/resources/"+id+"/tags", in, 200)
	admin.call("PATCH", "/api/v1/admin/resources/"+id+"/tags", in, 409)
	var stage, edition string
	s.db.QueryRow(`SELECT stage,edition FROM learning_documents WHERE id=$1`, id).Scan(&stage, &edition)
	if stage != "B" || edition != "" {
		t.Fatal(stage, edition)
	}
	got, err := s.documentPDF(ctx, id, "question")
	if err != nil || string(got) != string(pdf) {
		t.Fatal("original changed", err)
	}
	admin.call("GET", "/api/v1/admin/resources?stage=B", nil, 200)
}
func TestResourceLeaseFencesLateWrites(t *testing.T) {
	s, _, _ := learningTestServer(t)
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	pdf, _ := os.ReadFile("testdata/questions.pdf")
	id, err := s.storeRawResource(ctx, "lease.pdf", pdf, nil, resourceSource{})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.enqueueLearning(ctx, "resource_classify", "lease-test", map[string]string{"document_id": id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE learning_jobs SET status='running',generation=1,lease_until=now()+interval '90 seconds' WHERE id=$1`, job); err != nil {
		t.Fatal(err)
	}
	lease := context.WithValue(ctx, jobLeaseKey{}, jobLease{job, 1, id, 1})
	tx, err := s.learningWriteTx(lease)
	if err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	s.db.Exec(`UPDATE learning_jobs SET cancel_requested=true WHERE id=$1`, job)
	if tx, err = s.learningWriteTx(lease); !errors.Is(err, errStaleJob) {
		if tx != nil {
			tx.Rollback()
		}
		t.Fatal("cancelled worker wrote", err)
	}
	s.db.Exec(`UPDATE learning_jobs SET cancel_requested=false,generation=2 WHERE id=$1`, job)
	if tx, err = s.learningWriteTx(lease); !errors.Is(err, errStaleJob) {
		if tx != nil {
			tx.Rollback()
		}
		t.Fatal("old generation wrote", err)
	}
	s.db.Exec(`UPDATE learning_jobs SET generation=1 WHERE id=$1`, job)
	s.db.Exec(`UPDATE learning_documents SET revision=2 WHERE id=$1`, id)
	if tx, err = s.learningWriteTx(lease); !errors.Is(err, errStaleJob) {
		if tx != nil {
			tx.Rollback()
		}
		t.Fatal("old resource revision wrote", err)
	}
}
func TestResourceScanRecoveryAndControl(t *testing.T) {
	s, _, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	dir := t.TempDir()
	t.Setenv("LEARNING_PDF_DIR", dir)
	pdf, _ := os.ReadFile("testdata/questions.pdf")
	os.WriteFile(dir+"/sample.pdf", pdf, 0600)
	past := time.Now().Add(-time.Minute)
	os.Chtimes(dir+"/sample.pdf", past, past)
	j := admin.call("POST", "/api/v1/admin/resources/scans", map[string]any{"directory": ".", "recursive": true}, 202)
	id := rawString(j, "job_id")
	admin.call("POST", "/api/v1/admin/resources/jobs/"+id+"/pause", map[string]int{"revision": 1}, 202)
	admin.call("POST", "/api/v1/admin/resources/jobs/"+id+"/resume", map[string]int{"revision": 1}, 409)
	admin.call("POST", "/api/v1/admin/resources/jobs/"+id+"/resume", map[string]int{"revision": 2}, 202)
	if err := s.processLearningJob(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	var result json.RawMessage
	s.db.QueryRow(`SELECT status,result FROM learning_jobs WHERE id=$1`, id).Scan(&status, &result)
	if status != "completed" {
		t.Fatal(status, string(result))
	}
	// A stale running lease is reclaimed; rescanning content is idempotent.
	s.db.Exec(`UPDATE learning_jobs SET status='running',lease_until=now()-interval '1 second',generation=generation+1 WHERE id=$1`, id)
	if err := s.processLearningJob(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM learning_resource_files`).Scan(&count)
	if count != 1 {
		t.Fatal("restart duplicated resources", count)
	}
	admin.call("POST", "/api/v1/admin/resources/scans", map[string]string{"directory": "../"}, 400)
}

func TestAllRoutesRegisterWithoutAmbiguity(t *testing.T) { _ = (&server{}).routes() }
