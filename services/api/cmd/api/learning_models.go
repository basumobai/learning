package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"example.com/els/api/internal/security"
)

// A user's selection is an ID reference, never a provider URL or credential.
type learningModelSelection struct {
	ModelID     int64   `json:"model_id"`
	ReviewerIDs []int64 `json:"reviewer_ids"`
	LeadModelID int64   `json:"lead_model_id"`
}

func (s *server) validateLearningSelection(ctx context.Context, choice *learningModelSelection) error {
	if choice == nil {
		return nil
	}
	if choice.ModelID < 0 || choice.LeadModelID < 0 || len(choice.ReviewerIDs) > 5 {
		return fmt.Errorf("模型选择无效")
	}
	seen := map[int64]bool{}
	for _, id := range choice.ReviewerIDs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("评审模型无效或重复")
		}
		seen[id] = true
	}
	for _, id := range append(append([]int64{}, choice.ReviewerIDs...), choice.ModelID, choice.LeadModelID) {
		if id == 0 {
			continue
		}
		// Even an administrator using the student workspace selects from their identity group.
		if _, err := s.allowedModel(ctx, id, "chat"); err != nil {
			return fmt.Errorf("模型未获身份组授权或已停用，请刷新模型列表")
		}
	}
	return nil
}
func (s *server) selectedLearningModels(ctx context.Context, task string, personal bool, choice *learningModelSelection) ([]aiModel, aiModel, error) {
	if personal || choice == nil {
		return s.taskModels(ctx, task, personal)
	}
	if err := s.validateLearningSelection(ctx, choice); err != nil {
		return nil, aiModel{}, err
	}
	ids := []int64{choice.ModelID}
	leadID := choice.ModelID
	if task == "essay_reviewer" {
		ids = choice.ReviewerIDs
		leadID = choice.LeadModelID
		if len(ids) < 2 || leadID == 0 {
			return nil, aiModel{}, fmt.Errorf("作文请从身份组中选择2至5个评审模型和1个主审")
		}
	} else if choice.ModelID == 0 {
		return nil, aiModel{}, fmt.Errorf("请先选择身份组中的助学模型")
	}
	result := []aiModel{}
	for _, id := range ids {
		m, err := s.allowedModel(ctx, id, "chat")
		if err != nil {
			return nil, aiModel{}, err
		}
		m.budgetTask = task
		result = append(result, m)
	}
	lead, err := s.allowedModel(ctx, leadID, "chat")
	lead.budgetTask = task
	if task == "essay_reviewer" {
		lead.budgetTask = "essay_lead"
	}
	return result, lead, err
}
func learningSelectionSnapshot(task string, models []aiModel, lead aiModel) *learningModelSelection {
	choice := &learningModelSelection{ModelID: models[0].ID}
	if task == "essay_reviewer" {
		choice.LeadModelID = lead.ID
		for _, m := range models {
			choice.ReviewerIDs = append(choice.ReviewerIDs, m.ID)
		}
	}
	return choice
}
func learningModelsKey(models []aiModel, lead aiModel) string {
	data := []string{}
	for _, m := range append(append([]aiModel{}, models...), lead) {
		data = append(data, fmt.Sprintf("%d:%s:%s:%s", m.ID, m.ModelID, m.baseURL, m.version))
	}
	raw, _ := json.Marshal(data)
	return security.Digest(string(raw))
}
func (s *server) learningModelOptions(w http.ResponseWriter, r *http.Request) {
	var groupID int64
	var groupName string
	if err := s.db.QueryRowContext(r.Context(), `SELECT g.id,g.name FROM users u JOIN permission_groups g ON g.id=u.group_id WHERE u.id=$1`, userID(r.Context())).Scan(&groupID, &groupName); err != nil {
		s.dbError(w, "model group", err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT m.id,m.name,m.model_id,p.name FROM ai_models m JOIN ai_providers p ON p.id=m.provider_id JOIN group_models gm ON gm.model_id=m.id WHERE gm.group_id=$1 AND m.kind='chat' AND m.enabled AND p.enabled ORDER BY m.name,m.id`, groupID)
	if err != nil {
		s.dbError(w, "group models", err)
		return
	}
	models := []aiModel{}
	allowed := map[int64]bool{}
	for rows.Next() {
		m := aiModel{Kind: "chat"}
		if err = rows.Scan(&m.ID, &m.Name, &m.ModelID, &m.ProviderName); err != nil {
			break
		}
		models = append(models, m)
		allowed[m.ID] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		s.dbError(w, "group model row", err)
		return
	}
	choice := learningModelSelection{ReviewerIDs: []int64{}}
	var raw []byte
	if s.db.QueryRowContext(r.Context(), `SELECT selection FROM learning_model_preferences WHERE user_id=$1`, userID(r.Context())).Scan(&raw) == nil {
		_ = json.Unmarshal(raw, &choice)
	}
	if !allowed[choice.ModelID] {
		choice.ModelID = 0
		var defaults []byte
		var ids []int64
		if s.db.QueryRowContext(r.Context(), `SELECT model_ids FROM learning_ai_tasks WHERE task='explanation'`).Scan(&defaults) == nil {
			_ = json.Unmarshal(defaults, &ids)
		}
		for _, id := range ids {
			if allowed[id] {
				choice.ModelID = id
				break
			}
		}
		if choice.ModelID == 0 && len(models) > 0 {
			choice.ModelID = models[0].ID
		}
	}
	valid := []int64{}
	seen := map[int64]bool{}
	for _, id := range choice.ReviewerIDs {
		if allowed[id] && !seen[id] {
			valid = append(valid, id)
			seen[id] = true
		}
	}
	choice.ReviewerIDs = valid
	if len(valid) < 2 {
		var defaults []byte
		var ids []int64
		var lead *int64
		if s.db.QueryRowContext(r.Context(), `SELECT model_ids,lead_model_id FROM learning_ai_tasks WHERE task='essay_reviewer'`).Scan(&defaults, &lead) == nil {
			_ = json.Unmarshal(defaults, &ids)
			for _, id := range ids {
				if allowed[id] && !seen[id] && len(choice.ReviewerIDs) < 5 {
					choice.ReviewerIDs = append(choice.ReviewerIDs, id)
					seen[id] = true
				}
			}
			if lead != nil && allowed[*lead] && !allowed[choice.LeadModelID] {
				choice.LeadModelID = *lead
			}
		}
	}
	if !allowed[choice.LeadModelID] {
		choice.LeadModelID = choice.ModelID
	}
	var personal bool
	_ = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_personal_api WHERE user_id=$1 AND enabled)`, userID(r.Context())).Scan(&personal)
	respond(w, 200, map[string]any{"group": map[string]any{"id": groupID, "name": groupName}, "models": models, "selection": choice, "personal_enabled": personal})
}
func (s *server) saveLearningModelPreferences(w http.ResponseWriter, r *http.Request) {
	var choice learningModelSelection
	if decodeLimit(r, &choice, 4000) != nil {
		fail(w, 400, "模型选择格式无效")
		return
	}
	if err := s.validateLearningSelection(r.Context(), &choice); err != nil {
		fail(w, 403, err.Error())
		return
	}
	raw, _ := json.Marshal(choice)
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO learning_model_preferences(user_id,selection) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET selection=$2,updated_at=now()`, userID(r.Context()), string(raw)); err != nil {
		s.dbError(w, "model preferences", err)
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}

// Subjective answers are not marked wrong merely because their wording differs.
func learningExplanationNeeded(a learningAttempt) bool {
	if a.Submitted == nil {
		return false
	}
	var f struct {
		Parts []struct {
			Correct bool `json:"correct"`
			NeedsAI bool `json:"needs_ai"`
		} `json:"parts"`
		AIGrading *gradeResult `json:"ai_grading"`
	}
	_ = json.Unmarshal(a.Feedback, &f)
	for _, p := range f.Parts {
		if !p.NeedsAI && !p.Correct {
			return true
		}
	}
	if f.AIGrading != nil {
		for _, p := range f.AIGrading.Parts {
			for _, part := range a.Question.Content.Parts {
				if p.ID == part.ID && p.Score < part.Points {
					return true
				}
			}
		}
	}
	return false
}
