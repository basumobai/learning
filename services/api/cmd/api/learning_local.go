package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func localPDFRoot() string {
	if dir := os.Getenv("LEARNING_PDF_DIR"); dir != "" {
		return dir
	}
	return "/data/pdf"
}

// os.Root confines reads even if a symlink is changed between checking and opening.
func readLocalPDF(dir, name string) ([]byte, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00") || !strings.EqualFold(filepath.Ext(name), ".pdf") {
		return nil, fmt.Errorf("请选择资料目录内的 PDF 文件名")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("资料目录不可读，请检查只读挂载与文件权限")
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("PDF 不存在或路径超出资料目录")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > maxPDF {
		return nil, fmt.Errorf("PDF 需为32MB以内的普通文件")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPDF+1))
	if err != nil || len(data) > maxPDF || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, fmt.Errorf("PDF 无效、读取失败或超过32MB")
	}
	return data, nil
}
func (s *server) localLearningFiles(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(localPDFRoot())
	if err != nil {
		fail(w, 503, "资料目录不可读；请创建 data/pdf，放入PDF后检查只读挂载与权限")
		return
	}
	files := []map[string]any{}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, map[string]any{"name": e.Name(), "size": info.Size(), "importable": info.Size() <= maxPDF})
		if len(files) >= 500 {
			break
		}
	}
	host := os.Getenv("LEARNING_PDF_HOST_DIR")
	if host == "" {
		host = "./data/pdf"
	}
	respond(w, 200, map[string]any{"files": files, "host_directory": host, "container_directory": localPDFRoot(), "limit": 500})
}
func (s *server) importLocalPDF(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title    string `json:"title"`
		Subject  string `json:"subject_id"`
		Textbook string `json:"textbook"`
		Term     string `json:"term"`
		Question string `json:"question_path"`
		Answer   string `json:"answer_path"`
		Password string `json:"admin_password"`
	}
	if decode(r, &in) != nil {
		fail(w, 400, "导入信息无效")
		return
	}
	if !s.adminPassword(w, r, in.Password) {
		return
	}
	q, err := readLocalPDF(localPDFRoot(), in.Question)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	a, err := readLocalPDF(localPDFRoot(), in.Answer)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.storeLearningDocument(w, r, in.Title, in.Subject, in.Textbook, in.Term, q, a)
}
