package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func extractWriting(name string, data []byte) (string, error) {
	if len(data) > 2*1024*1024 {
		return "", fmt.Errorf("作文文件最多2MB")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".txt" {
		if !utf8.Valid(data) {
			return "", fmt.Errorf("TXT请保存为UTF-8编码")
		}
		return strings.TrimPrefix(string(data), "\ufeff"), nil
	}
	if ext != ".docx" {
		return "", fmt.Errorf("支持.docx和UTF-8 .txt；旧版.doc请另存为.docx")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("Word文件无效")
	}
	for _, file := range z.File {
		if file.Name != "word/document.xml" {
			continue
		}
		if file.UncompressedSize64 > 2*1024*1024 {
			return "", fmt.Errorf("Word内容过大")
		}
		reader, err := file.Open()
		if err != nil {
			return "", err
		}
		defer reader.Close()
		dec := xml.NewDecoder(io.LimitReader(reader, 2*1024*1024))
		var out strings.Builder
		inText := false
		for {
			token, e := dec.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", fmt.Errorf("Word文档XML无效")
			}
			switch v := token.(type) {
			case xml.StartElement:
				if v.Name.Local == "t" {
					inText = true
				}
			case xml.EndElement:
				if v.Name.Local == "t" {
					inText = false
				}
				if v.Name.Local == "p" {
					out.WriteString("\n")
				}
			case xml.CharData:
				if inText {
					out.Write(v)
				}
			}
		}
		return out.String(), nil
	}
	return "", fmt.Errorf("Word文档没有正文")
}
func (s *server) learningTextImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024+65536)
	if r.ParseMultipartForm(512*1024) != nil {
		fail(w, 400, "文件格式无效或过大")
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, h, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "请选择作文文件")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 2*1024*1024+1))
	if err != nil {
		fail(w, 400, "读取失败")
		return
	}
	text, err := extractWriting(h.Filename, data)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if len([]rune(text)) > 12000 {
		fail(w, 400, "正文最多12000字符")
		return
	}
	respond(w, 200, map[string]string{"text": text})
}
