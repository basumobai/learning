package main

import (
	"bytes"
	"encoding/json"
	"example.com/els/api/internal/security"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *server) recordingRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/v1/learning/recordings", s.uploadRecording)
	m.HandleFunc("GET /api/v1/learning/recordings/{id}/audio", s.recordingAudio)
	m.HandleFunc("POST /api/v1/learning/recordings/{id}/transcribe", s.transcribeRecording)
}
func audioExtension(data []byte) (string, string) {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return "audio/wav", "wav"
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return "audio/webm", "webm"
	}
	if len(data) >= 4 && string(data[:4]) == "OggS" {
		return "audio/ogg", "ogg"
	}
	if len(data) >= 8 && string(data[4:8]) == "ftyp" {
		return "audio/mp4", "m4a"
	}
	if len(data) >= 3 && (string(data[:3]) == "ID3" || data[0] == 0xff && data[1]&0xe0 == 0xe0) {
		return "audio/mpeg", "mp3"
	}
	return "", ""
}
func (s *server) uploadRecording(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 21*1024*1024)
	if err := r.ParseMultipartForm(1024 * 1024); err != nil {
		fail(w, 400, "录音应小于20MB")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	request, training := r.FormValue("request_id"), r.FormValue("training_id")
	index, err := strconv.Atoi(r.FormValue("task_index"))
	if !requestIDPattern.MatchString(request) || err != nil || index < 0 || index > 30 {
		fail(w, 400, "录音来源无效")
		return
	}
	var raw []byte
	if s.db.QueryRowContext(r.Context(), `SELECT payload FROM training_sessions WHERE id=$1 AND user_id=$2 AND kind='listening'`, training, userID(r.Context())).Scan(&raw) != nil {
		fail(w, 404, "听说练习不存在")
		return
	}
	var lesson listeningLesson
	if json.Unmarshal(raw, &lesson) != nil || index >= len(lesson.SpeakingTasks) {
		fail(w, 400, "录音必须对应本人的听说任务")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "请选择录音文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 20*1024*1024+1))
	if err != nil || len(data) > 20*1024*1024 {
		fail(w, 400, "录音超出20MB")
		return
	}
	mime, _ := audioExtension(data)
	if mime == "" {
		fail(w, 400, "支持WebM、Ogg、WAV、M4A或MP3录音")
		return
	}
	hash := security.Digest(string(data))
	digest := payloadDigest([]any{training, index, hash})
	id := security.Token(18)
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO learning_recordings(id,user_id,training_id,task_index,mime,audio,hash,request_id,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(user_id,request_id) DO NOTHING`, id, userID(r.Context()), training, index, mime, data, hash, request, digest)
	var prior string
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT id,digest FROM learning_recordings WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), request).Scan(&id, &prior)
	}
	if err != nil {
		s.dbError(w, "save recording", err)
		return
	}
	if prior != digest {
		fail(w, 409, "录音请求标识冲突")
		return
	}
	respond(w, 201, map[string]string{"id": id, "mime": mime, "note": "学生录音原件；尚未做发音评分"})
}
func (s *server) recordingAudio(w http.ResponseWriter, r *http.Request) {
	var data []byte
	var mime string
	if s.db.QueryRowContext(r.Context(), `SELECT audio,mime FROM learning_recordings WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context())).Scan(&data, &mime) != nil {
		fail(w, 404, "录音不存在")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline")
	w.Write(data)
}
func (s *server) transcribeRecording(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request string `json:"request_id"`
		Model   int64  `json:"model_id"`
	}
	if decode(r, &in) != nil || !requestIDPattern.MatchString(in.Request) {
		fail(w, 400, "转写请求无效")
		return
	}
	var data []byte
	if s.db.QueryRowContext(r.Context(), `SELECT audio FROM learning_recordings WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context())).Scan(&data) != nil {
		fail(w, 404, "录音不存在")
		return
	}
	model, err := s.allowedModel(r.Context(), in.Model, "asr")
	if err != nil {
		fail(w, 403, "请管理员配置并授权兼容/audio/transcriptions的ASR模型")
		return
	}
	if s.aiReplay(w, r, in.Request, "transcription:"+r.PathValue("id"), "", in) {
		return
	}
	complete := false
	defer func() {
		if !complete {
			s.failAI(r.Context(), in.Request)
		}
	}()
	if err = validateProviderURL(model.baseURL); err != nil {
		fail(w, 400, "语音服务地址无效")
		return
	}
	_, extension := audioExtension(data)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "recording."+extension)
	if err != nil {
		s.dbError(w, "audio multipart", err)
		return
	}
	part.Write(data)
	writer.WriteField("model", model.ModelID)
	writer.WriteField("language", "en")
	writer.WriteField("response_format", "json")
	writer.Close()
	request, err := http.NewRequestWithContext(r.Context(), "POST", model.baseURL+"/audio/transcriptions", &body)
	if err != nil {
		s.dbError(w, "transcription request", err)
		return
	}
	request.Header.Set("Authorization", "Bearer "+model.key)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	release, slotErr := takeAISlot(userID(r.Context()))
	if slotErr != nil {
		fail(w, 503, "语音转写正在处理，请稍后重试")
		return
	}
	defer release()
	client := s.client
	if client == nil {
		client = newProviderClient()
	}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		providerFailure(w, err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		providerFailure(w, upstreamError{response.StatusCode})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		fail(w, 502, "转写服务响应过大")
		return
	}
	var out struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &out) != nil || strings.TrimSpace(out.Text) == "" || len([]rune(out.Text)) > 12000 {
		fail(w, 502, "转写服务没有返回有效文本")
		return
	}
	current, err := s.allowedModel(r.Context(), in.Model, "asr")
	if err != nil || evaluationModelVersion(current) != evaluationModelVersion(model) {
		fail(w, 403, "转写模型授权或配置已改变")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "save transcription", err)
		return
	}
	defer tx.Rollback()
	id := security.Token(18)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_transcriptions(id,recording_id,user_id,model_id,model_version,text,request_id,digest,elapsed_ms) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, r.PathValue("id"), userID(r.Context()), in.Model, evaluationModelVersion(model), out.Text, in.Request, payloadDigest(in), min(time.Since(started).Milliseconds(), 2147483647))
	result := map[string]any{"id": id, "text": out.Text, "confidence": "asr_unverified", "note": "自动转写需要核对；不代表官方听说或发音评分"}
	if err == nil {
		err = finishAI(r.Context(), tx, in.Request, result)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "persist transcription", err)
		return
	}
	complete = true
	respond(w, 200, result)
}
