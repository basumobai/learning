package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type server struct {
	db            *sql.DB
	origin        string
	secureCookie  bool
	encryptionKey []byte
	client        *http.Client
	redis         *redis.Client
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal("database configuration invalid; check DATABASE_URL without sharing credentials")
	}
	db.SetMaxOpenConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < 30; attempt++ {
		if err = db.PingContext(ctx); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatal("database unavailable or authentication failed; restore the original .env and see docs/DEPLOY.md; database has not been reset")
	}
	if len(os.Args) > 1 && os.Args[1] == "admin-reset-password" {
		if err := adminRecoveryCommand(context.Background(), db, os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "worker" || os.Args[1] == "worker-health") {
		s, err := configure(db)
		if err != nil {
			log.Fatal(err)
		}
		if os.Args[1] == "worker-health" {
			var alive bool
			if s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM learning_worker_heartbeats WHERE updated_at>now()-interval '45 seconds')`).Scan(&alive) != nil || !alive {
				os.Exit(1)
			}
			return
		}
		workerCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ready := false
		for attempt := 0; attempt < 120; attempt++ {
			if err = s.db.QueryRowContext(workerCtx, `SELECT to_regclass('learning_worker_heartbeats') IS NOT NULL`).Scan(&ready); err == nil && ready {
				break
			}
			select {
			case <-workerCtx.Done():
				return
			case <-time.After(time.Second):
			}
		}
		if !ready {
			log.Fatal("worker waiting for API migrations timed out")
		}
		s.learningWorkers(workerCtx)
		<-workerCtx.Done()
		return
	}
	if err := migrate(ctx, db); err != nil {
		log.Fatal(err)
	}
	s, err := configure(db)
	if err != nil {
		log.Fatal(err)
	}
	if err := s.bootstrap(context.Background()); err != nil {
		log.Fatal(err)
	}
	if err := s.initLearning(context.Background()); err != nil {
		log.Fatal(err)
	}
	if os.Getenv("LEARNING_EMBEDDED_WORKER") == "true" {
		s.learningWorkers(context.Background())
	}
	mux := s.routes()
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("Learning API listening on %s", addr)
	log.Fatal((&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 120 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 * 1024}).ListenAndServe())
}

func migrate(ctx context.Context, db *sql.DB) error {
	lock, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	if _, err = lock.ExecContext(ctx, `SELECT pg_advisory_lock(19283742)`); err != nil {
		return err
	}
	defer lock.ExecContext(context.Background(), `SELECT pg_advisory_unlock(19283742)`)
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "/app/migrations"
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no migrations found")
	}
	for _, path := range paths {
		name := filepath.Base(path)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var exists bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists)
		if err != nil {
			tx.Rollback()
			return err
		}
		if exists {
			tx.Rollback()
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			tx.Rollback()
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func respond(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(r *http.Request, dst any) error {
	return decodeLimit(r, dst, 4096)
}
func decodeLimit(r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(nil, r.Body, limit)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("extra JSON")
		}
		return err
	}
	return nil
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		fail(w, 503, "database unavailable")
		return
	}
	respond(w, 200, map[string]string{"status": "ok"})
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/dashboard", s.dashboard)
	mux.HandleFunc("POST /api/v1/study/sessions", s.createSession)
	mux.HandleFunc("GET /api/v1/study/sessions/active", s.activeSession)
	mux.HandleFunc("GET /api/v1/study/sessions/{id}", s.getSession)
	mux.HandleFunc("POST /api/v1/study/sessions/{id}/answers", s.answer)
	mux.HandleFunc("POST /api/v1/study/sessions/{id}/review", s.review)
	mux.HandleFunc("POST /api/v1/study/sessions/{id}/undo", s.undo)
	s.extraRoutes(mux)
	s.modelCatalogRoutes(mux)
	s.resourceRoutes(mux)
	s.shareRoutes(mux)
	s.deepRoutes(mux)
	s.learningRoutes(mux)
	return s.protect(mux)
}
func (s *server) dbError(w http.ResponseWriter, action string, err error) {
	log.Printf("%s: %v", action, err)
	fail(w, 500, "操作暂不可用，请稍后重试")
}
