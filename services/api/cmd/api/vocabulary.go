package main

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"example.com/els/api/internal/security"
	"example.com/els/api/internal/vocabulary"
)

type importInput struct {
	Text      string `json:"text"`
	RequestID string `json:"request_id"`
	Overwrite bool   `json:"overwrite_phonetic"`
	Password  string `json:"admin_password"`
}

func (s *server) importPreview(w http.ResponseWriter, r *http.Request) {
	var input importInput
	if decodeLimit(r, &input, 2*1024*1024) != nil {
		fail(w, 400, "导入内容过大或格式无效")
		return
	}
	respond(w, 200, vocabulary.Parse(input.Text))
}
func (s *server) importVocabulary(w http.ResponseWriter, r *http.Request) {
	var input importInput
	if decodeLimit(r, &input, 2*1024*1024) != nil || !requestIDPattern.MatchString(input.RequestID) {
		fail(w, 400, "导入请求无效")
		return
	}
	preview := vocabulary.Parse(input.Text)
	if preview.Valid() != nil {
		respond(w, 400, preview)
		return
	}
	if !s.adminPassword(w, r, input.Password) {
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "import transaction", err)
		return
	}
	defer tx.Rollback()
	fingerprint := security.Digest(input.Text + strconv.FormatBool(input.Overwrite))
	var hash string
	var added, skipped int
	err = tx.QueryRowContext(r.Context(), `SELECT content_hash,added,skipped FROM vocabulary_imports WHERE actor_id=$1 AND request_id=$2`, userID(r.Context()), input.RequestID).Scan(&hash, &added, &skipped)
	if err == nil {
		if hash != fingerprint {
			fail(w, 409, "导入请求编号已使用")
			return
		}
		respond(w, 200, map[string]any{"added": added, "skipped": skipped, "replayed": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.dbError(w, "import replay", err)
		return
	}
	// Stable lock order also makes overlapping imports by different administrators safe.
	sort.Slice(preview.Entries, func(i, j int) bool { return preview.Entries[i].Word < preview.Entries[j].Word })
	skipped = preview.Duplicates
	for _, entry := range preview.Entries {
		var word, sense int64
		err = tx.QueryRowContext(r.Context(), `INSERT INTO words(lemma) VALUES($1) ON CONFLICT(lemma) DO UPDATE SET lemma=EXCLUDED.lemma RETURNING id`, entry.Word).Scan(&word)
		if err != nil {
			s.dbError(w, "import word", err)
			return
		}
		err = tx.QueryRowContext(r.Context(), `INSERT INTO word_senses(word_id,part_of_speech,meaning_zh,phonetic) VALUES($1,$2,$3,$4) ON CONFLICT(word_id,part_of_speech,meaning_zh) DO NOTHING RETURNING id`, word, entry.POS, entry.Chinese, entry.Phonetic).Scan(&sense)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(r.Context(), `UPDATE word_senses SET phonetic=$4 WHERE word_id=$1 AND part_of_speech=$2 AND meaning_zh=$3 AND (phonetic='' OR $5)`, word, entry.POS, entry.Chinese, entry.Phonetic, input.Overwrite)
			if err != nil {
				s.dbError(w, "import phonetic", err)
				return
			}
			skipped++
			continue
		}
		if err != nil {
			s.dbError(w, "import sense", err)
			return
		}
		added++
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO vocabulary_imports(actor_id,request_id,content_hash,added,skipped) VALUES($1,$2,$3,$4,$5)`, userID(r.Context()), input.RequestID, fingerprint, added, skipped); err == nil {
		err = audit(tx, r, "import_vocabulary", strconv.Itoa(added))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "import commit", err)
		return
	}
	respond(w, 200, map[string]int{"added": added, "skipped": skipped})
}
func (s *server) vocabulary(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if len(query) > 200 {
		fail(w, 400, "搜索内容过长")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		fail(w, 400, "页码无效")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT s.id,w.lemma,s.phonetic,s.part_of_speech,s.meaning_zh,EXISTS(SELECT 1 FROM suspended_senses x WHERE x.user_id=$1 AND x.sense_id=s.id),
 COALESCE((SELECT SUM(attempts-correct_count) FROM review_states rs WHERE rs.user_id=$1 AND rs.sense_id=s.id),0)
 FROM word_senses s JOIN words w ON w.id=s.word_id WHERE $2='' OR w.lemma ILIKE '%'||$2||'%' OR s.meaning_zh ILIKE '%'||$2||'%' ORDER BY w.lemma,s.id LIMIT 50 OFFSET $3`, userID(r.Context()), query, (page-1)*50)
	if err != nil {
		s.dbError(w, "vocabulary", err)
		return
	}
	defer rows.Close()
	entries := []map[string]any{}
	for rows.Next() {
		var id int64
		var word, phonetic, pos, chinese string
		var suspended bool
		var failures int
		if err = rows.Scan(&id, &word, &phonetic, &pos, &chinese, &suspended, &failures); err != nil {
			s.dbError(w, "vocabulary row", err)
			return
		}
		entries = append(entries, map[string]any{"id": id, "word": word, "phonetic": phonetic, "pos": pos, "chinese": chinese, "suspended": suspended, "failed_recalls": failures})
	}
	if rows.Err() != nil {
		s.dbError(w, "vocabulary rows", rows.Err())
		return
	}
	respond(w, 200, map[string]any{"entries": entries, "page": page})
}
func (s *server) suspendSense(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SenseID   int64 `json:"sense_id"`
		Suspended bool  `json:"suspended"`
	}
	if decode(r, &input) != nil || input.SenseID < 1 {
		fail(w, 400, "词条编号无效")
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "suspend", err)
		return
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM word_senses WHERE id=$1)`, input.SenseID).Scan(&exists); err != nil {
		s.dbError(w, "suspend sense", err)
		return
	}
	if !exists {
		fail(w, 404, "词条不存在")
		return
	}
	if input.Suspended {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO suspended_senses(user_id,sense_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID(r.Context()), input.SenseID)
	} else {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM suspended_senses WHERE user_id=$1 AND sense_id=$2`, userID(r.Context()), input.SenseID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "suspend commit", err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
