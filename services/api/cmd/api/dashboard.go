package main

import "net/http"

func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prefs, err := readPreferences(ctx, s.db)
	if err != nil {
		s.dbError(w, "dashboard preferences", err)
		return
	}
	introduced, err := newStudiedToday(ctx, s.db, prefs.Timezone)
	if err != nil {
		s.dbError(w, "dashboard budget", err)
		return
	}
	var due, seen, words, today, correct, wrong, pending, hints int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE next_review_at<=now() AND NOT EXISTS(SELECT 1 FROM suspended_senses x WHERE x.user_id=$1 AND x.sense_id=review_states.sense_id)),COUNT(DISTINCT sense_id) FROM review_states WHERE user_id=$1`, userID(ctx)).Scan(&due, &seen)
	if err == nil {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM words`).Scan(&words)
	}
	if err == nil {
		err = s.db.QueryRowContext(ctx, `SELECT
 COUNT(*) FILTER (WHERE rating IS NOT NULL),COUNT(*) FILTER (WHERE result='correct' AND rating>1 AND NOT used_hint),
 COUNT(*) FILTER (WHERE rating=1),COUNT(*) FILTER (WHERE rating IS NULL),COUNT(*) FILTER (WHERE used_hint)
 FROM answer_logs WHERE user_id=$1 AND undone_at IS NULL AND
 COALESCE(reviewed_at,created_at)>=(date_trunc('day',now() AT TIME ZONE $2) AT TIME ZONE $2)`, userID(ctx), prefs.Timezone).Scan(&today, &correct, &wrong, &pending, &hints)
	}
	if err != nil {
		s.dbError(w, "dashboard", err)
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT dimension,COUNT(*) FILTER (WHERE next_review_at<=now() AND NOT EXISTS(SELECT 1 FROM suspended_senses x WHERE x.user_id=$1 AND x.sense_id=review_states.sense_id)),COUNT(*),COALESCE(SUM(attempts),0),COALESCE(SUM(correct_count),0)
 FROM review_states WHERE user_id=$1 GROUP BY dimension ORDER BY dimension`, userID(ctx))
	if err != nil {
		s.dbError(w, "dashboard dimensions", err)
		return
	}
	defer rows.Close()
	dimensions := []map[string]any{}
	for rows.Next() {
		var kind string
		var nDue, nCards, attempts, success int
		if err = rows.Scan(&kind, &nDue, &nCards, &attempts, &success); err != nil {
			s.dbError(w, "read dimensions", err)
			return
		}
		dimensions = append(dimensions, map[string]any{"type": kind, "due": nDue, "cards": nCards, "attempts": attempts, "unassisted_correct": success})
	}
	if rows.Err() != nil {
		s.dbError(w, "read dimensions", rows.Err())
		return
	}
	respond(w, 200, map[string]any{"display_name": account(ctx).DisplayName, "due": due, "studied": seen, "demo_words": words, "today": map[string]int{"reviewed": today, "unassisted_correct": correct, "again": wrong, "awaiting_rating": pending, "used_hint": hints}, "dimensions": dimensions, "scheduler": "FSRS-6", "timezone": prefs.Timezone, "new_today": introduced, "new_remaining": max(0, prefs.DailyNew-introduced), "preferences": prefs})
}
