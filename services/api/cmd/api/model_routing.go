package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"example.com/els/api/internal/security"
)

type routePolicy struct {
	Fallback bool     `json:"fallback_enabled"`
	Attempts int      `json:"max_attempts"`
	Timeout  int      `json:"timeout_seconds"`
	Input    int      `json:"max_input_tokens"`
	Output   int      `json:"max_output_tokens"`
	Cost     *float64 `json:"max_cost_usd"`
}

func defaultRoutePolicy() routePolicy {
	return routePolicy{Attempts: 3, Timeout: 90, Input: 32000, Output: 4000}
}
func (p routePolicy) validate() error {
	if p.Attempts < 1 || p.Attempts > 8 || p.Timeout < 5 || p.Timeout > 120 || p.Input < 256 || p.Input > 2000000 || p.Output < 16 || p.Output > 32000 || p.Cost != nil && (*p.Cost <= 0 || *p.Cost > 100) {
		return fmt.Errorf("任务预算或重试参数无效")
	}
	return nil
}
func (s *server) routePolicy(ctx context.Context, task string) routePolicy {
	p := defaultRoutePolicy()
	var raw []byte
	if s.db != nil && s.db.QueryRowContext(ctx, `SELECT routing FROM learning_ai_tasks WHERE task=$1`, task).Scan(&raw) == nil {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// Concurrency slots are transient, never daily/monthly user quotas.
var aiSlots = struct {
	sync.Mutex
	active int
	users  map[string]int
}{users: map[string]int{}}

func takeAISlot(user string) (func(), error) {
	aiSlots.Lock()
	defer aiSlots.Unlock()
	if aiSlots.active >= 16 || aiSlots.users[user] >= 6 {
		return nil, fmt.Errorf("AI并发请求较多，请稍后重试")
	}
	aiSlots.active++
	aiSlots.users[user]++
	return func() {
		aiSlots.Lock()
		defer aiSlots.Unlock()
		aiSlots.active--
		aiSlots.users[user]--
		if aiSlots.users[user] == 0 {
			delete(aiSlots.users, user)
		}
	}, nil
}

func inputTokenBound(v any) (int, bool) {
	// UTF-8 bytes conservatively bound text tokens. Image tokenization depends on
	// the provider: never present the image estimate as a guaranteed money bound.
	raw, _ := json.Marshal(v)
	var decoded any
	_ = json.Unmarshal(raw, &decoded)
	total := 256
	images := false
	var visit func(any)
	visit = func(x any) {
		switch q := x.(type) {
		case string:
			total += len([]byte(q))
		case []any:
			for _, e := range q {
				visit(e)
			}
		case map[string]any:
			for k, e := range q {
				if k == "image_url" {
					images = true
					total += 4096
					continue
				}
				visit(e)
			}
		}
	}
	visit(decoded)
	return total, images
}
func (s *server) checkCallBudget(ctx context.Context, m aiModel, body any) (int, error) {
	bound, images := inputTokenBound(body)
	p := s.routePolicy(ctx, modelBudgetTask(m))
	output := p.Output
	if m.maxOutput > 0 {
		output = m.maxOutput
	}
	if bound > p.Input {
		return bound, fmt.Errorf("输入超过任务token预算，请缩小处理范围")
	}
	var modalities []byte
	var contextLength int64
	if s.db != nil && m.ID > 0 {
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(c.input_modalities,'null'),COALESCE(c.context_length,0) FROM ai_models m JOIN ai_model_catalog c ON c.provider_id=m.provider_id AND c.model_id=m.model_id WHERE m.id=$1 AND c.available`, m.ID).Scan(&modalities, &contextLength)
		var known []string
		if images && json.Unmarshal(modalities, &known) == nil && len(known) > 0 {
			vision := false
			for _, v := range known {
				if v == "image" {
					vision = true
				}
			}
			if !vision {
				return bound, fmt.Errorf("服务商目录显示模型不支持图像输入")
			}
		}
		if contextLength > 0 && int64(bound+output) > contextLength {
			return bound, fmt.Errorf("输入与输出预留超过模型上下文窗口")
		}
	}
	budget, _ := ctx.Value(processingBudgetKey{}).(*processingBudget)
	if budget != nil {
		if lease, ok := ctx.Value(jobLeaseKey{}).(jobLease); ok {
			result, err := s.db.ExecContext(ctx, `UPDATE learning_jobs SET ai_token_reserved=ai_token_reserved+$3 WHERE id=$1 AND generation=$2 AND status='running' AND NOT cancel_requested AND NOT pause_requested AND ai_token_reserved+$3<=$4`, lease.ID, lease.Generation, bound+output, budget.Tokens)
			if err != nil {
				return bound, err
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return bound, fmt.Errorf("累计AI token预留达到任务预算或任务已停止")
			}
		} else if err := budget.reserveTokens(bound + output); err != nil {
			return bound, err
		}
	}

	if p.Cost == nil && (budget == nil || budget.Cost == nil) {
		return bound, nil
	}
	if images {
		return bound, fmt.Errorf("图片计费缺少可验证上限，请调整任务预算策略")
	}
	var pricing []byte
	var unit string
	err := s.db.QueryRowContext(ctx, `SELECT c.pricing,COALESCE(c.price_unit,'') FROM ai_models m JOIN ai_model_catalog c ON c.provider_id=m.provider_id AND c.model_id=m.model_id WHERE m.id=$1 AND c.available AND c.fetched_at>now()-interval '24 hours'`, m.ID).Scan(&pricing, &unit)
	if err != nil || unit != "USD/token" {
		return bound, fmt.Errorf("模型价格未知，不能验证费用预算")
	}
	var prices map[string]string
	if json.Unmarshal(pricing, &prices) != nil {
		return bound, fmt.Errorf("模型价格格式未知")
	}
	a, e1 := strconv.ParseFloat(prices["prompt"], 64)
	b, e2 := strconv.ParseFloat(prices["completion"], 64)
	if e1 != nil || e2 != nil || a < 0 || b < 0 || math.IsNaN(a) || math.IsNaN(b) || math.IsInf(a, 0) || math.IsInf(b, 0) {
		return bound, fmt.Errorf("模型价格未提供")
	}
	cost := float64(bound)*a + float64(output)*b
	if p.Cost != nil && cost > *p.Cost {
		return bound, fmt.Errorf("预计费用超过单次任务预算")
	}
	if callBudget, ok := ctx.Value(routeAttemptBudgetKey{}).(*processingBudget); ok {
		if err := callBudget.reserveCost(cost); err != nil {
			return bound, err
		}
	}

	if budget != nil && budget.Cost != nil {
		if lease, ok := ctx.Value(jobLeaseKey{}).(jobLease); ok {
			result, err := s.db.ExecContext(ctx, `UPDATE learning_jobs SET ai_cost_reserved=ai_cost_reserved+$3 WHERE id=$1 AND generation=$2 AND status='running' AND NOT cancel_requested AND NOT pause_requested AND ai_cost_reserved+$3<=$4`, lease.ID, lease.Generation, cost, *budget.Cost)
			if err != nil {
				return bound, err
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return bound, fmt.Errorf("累计AI费用预留达到任务预算或任务已停止")
			}
		} else if err := budget.reserveCost(cost); err != nil {
			return bound, err
		}
	}

	return bound, nil
}
func (s *server) routedLearningCompletion(ctx context.Context, m aiModel, prompt string, content any) (string, error) {
	if m.routeTask == "" || m.ID < 1 {
		if m.budgetTask != "" {
			policy := s.routePolicy(ctx, m.budgetTask)
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(policy.Timeout)*time.Second)
			defer cancel()
			if m.maxOutput == 0 {
				m.maxOutput = policy.Output
			}
			if policy.Cost != nil {
				ctx = context.WithValue(ctx, routeAttemptBudgetKey{}, &processingBudget{Cost: policy.Cost})
			}
		}
		return s.singleLearningCompletion(ctx, m, prompt, content)
	}
	policy := s.routePolicy(ctx, m.routeTask)
	if policy.Cost != nil {
		ctx = context.WithValue(ctx, routeAttemptBudgetKey{}, &processingBudget{Cost: policy.Cost})
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.Timeout)*time.Second)
	defer cancel()
	ids := []int64{m.ID}
	if policy.Fallback {
		var raw []byte
		if s.db.QueryRowContext(ctx, `SELECT model_ids FROM learning_ai_tasks WHERE task=$1`, m.routeTask).Scan(&raw) == nil {
			var configured []int64
			_ = json.Unmarshal(raw, &configured)
			for _, id := range configured {
				if id != m.ID {
					ids = append(ids, id)
				}
			}
		}
	}
	var last error
	attempts := 0
	for _, id := range ids {
		if attempts >= policy.Attempts || ctx.Err() != nil {
			break
		}
		candidate, err := s.learningModel(ctx, id)
		if err != nil {
			last = fmt.Errorf("候选模型已停用或未授权")
			continue
		}
		candidate.routeTask = m.routeTask
		candidate.maxOutput = policy.Output
		attempts++
		text, err := s.singleLearningCompletion(ctx, candidate, prompt, content)
		if err == nil {
			return text, nil
		}
		last = err
		if !policy.Fallback {
			break
		}
		if e, ok := err.(upstreamError); ok && e.status != http.StatusTooManyRequests && e.status < 500 && e.status != 400 && e.status != 401 && e.status != 402 && e.status != 403 && e.status != 404 {
			break
		}
	}
	if last == nil {
		last = fmt.Errorf("任务超时或没有可用模型")
	}
	return "", last
}

func (s *server) recordInvocation(ctx context.Context, m aiModel, bound, limit int) (string, error) {
	if s.db == nil || m.ID < 1 {
		return "", nil
	}
	id := security.Token(18)
	task := modelBudgetTask(m)
	if task == "" {
		task = "direct"
	}
	var owner any
	if userID(ctx) != "" {
		owner = userID(ctx)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_invocations(id,owner_id,task,model_id,status,input_token_bound,output_limit) VALUES($1,$2,$3,$4,'running',$5,$6)`, id, owner, task, m.ID, bound, limit)
	return id, err
}
func (s *server) finishInvocation(ctx context.Context, id string, raw []byte, callErr error) {
	if id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var result struct {
		Usage json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(raw, &result)
	status, code := "completed", ""
	if callErr != nil {
		status = "failed"
		code = "provider_unavailable"
		if e, ok := callErr.(upstreamError); ok {
			code = strconv.Itoa(e.status)
		}
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE ai_invocations SET status=$2,usage=$3,error_code=$4,finished_at=now() WHERE id=$1`, id, status, nullableJSON(result.Usage), code)
}

// A conservative reservation includes failed calls and fallbacks. It never
// promises a verified monetary cap when provider pricing or image billing is unknown.
type processingBudgetKey struct{}
type processingBudget struct {
	sync.Mutex
	Tokens, Used int
	Cost         *float64
	Reserved     float64
}

func withProcessingBudget(ctx context.Context, tokens int, cost *float64) context.Context {
	return context.WithValue(ctx, processingBudgetKey{}, &processingBudget{Tokens: tokens, Cost: cost})
}
func (b *processingBudget) reserveTokens(n int) error {
	b.Lock()
	defer b.Unlock()
	if b.Used+n > b.Tokens {
		return fmt.Errorf("累计AI token预留达到任务预算")
	}
	b.Used += n
	return nil
}
func (b *processingBudget) reserveCost(n float64) error {
	b.Lock()
	defer b.Unlock()
	if b.Cost != nil && b.Reserved+n > *b.Cost {
		return fmt.Errorf("累计AI费用预留达到任务预算")
	}
	b.Reserved += n
	return nil
}

type routeAttemptBudgetKey struct{}

func modelBudgetTask(m aiModel) string {
	if m.routeTask != "" {
		return m.routeTask
	}
	return m.budgetTask
}
