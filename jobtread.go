package main

// Read-only JobTread access: loads a saved Jobs view (filter + sort) and returns
// the job ids in that order. Never writes to JobTread.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type jtSettings struct {
	KeyFile     string `json:"keyFile"`     // .env file holding JOBTREAD_GRANT_KEY (env var wins)
	OrgID       string `json:"orgId"`       // JobTread organization
	ViewID      string `json:"viewId"`      // saved Jobs view, e.g. PRODUCTION
	ViewName    string `json:"viewName"`    // shown in Preferences
	ScheduleURL string `json:"scheduleUrl"` // {id} is replaced by the job id
}

func defaultJT() jtSettings {
	return jtSettings{
		KeyFile:     filepath.Join(os.Getenv("USERPROFILE"), `OneDrive\Business\Documents\CODE\Construction-Corp\.env`),
		OrgID:       "22NwWhUAf6VB",
		ViewID:      "22NwWhVJbMaE",
		ViewName:    "👷‍♂️PRODUCTION",
		ScheduleURL: "https://app.jobtread.com/jobs/{id}/schedule",
	}
}

func (s *jtSettings) fillDefaults() {
	d := defaultJT()
	if s.KeyFile == "" {
		s.KeyFile = d.KeyFile
	}
	if s.OrgID == "" {
		s.OrgID = d.OrgID
	}
	if s.ViewID == "" {
		s.ViewID, s.ViewName = d.ViewID, d.ViewName
	}
	if s.ViewName == "" {
		s.ViewName = s.ViewID
	}
	if s.ScheduleURL == "" {
		s.ScheduleURL = d.ScheduleURL
	}
}

func jtKey(s jtSettings) (string, error) {
	if k := strings.TrimSpace(os.Getenv("JOBTREAD_GRANT_KEY")); k != "" {
		return k, nil
	}
	if k := protectedKey(); k != "" { // put there by the installer
		return k, nil
	}
	f, err := os.Open(s.KeyFile)
	if err != nil {
		return "", fmt.Errorf("JobTread key not found (set JOBTREAD_GRANT_KEY or keyFile)")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "JOBTREAD_GRANT_KEY="); ok {
			if v = strings.Trim(strings.TrimSpace(v), `"'`); v != "" {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("JOBTREAD_GRANT_KEY is empty in %s", filepath.Base(s.KeyFile))
}

var httpc = &http.Client{Timeout: 20 * time.Second}

func pave(key string, q map[string]any) (map[string]any, error) {
	q["$"] = map[string]any{"grantKey": key}
	body, _ := json.Marshal(map[string]any{"query": q})
	resp, err := httpc.Post("https://api.jobtread.com/pave", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("JobTread unreachable: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		msg := strings.TrimSpace(string(b))
		if len(msg) > 160 {
			msg = msg[:160]
		}
		return nil, fmt.Errorf("JobTread %d: %s", resp.StatusCode, msg)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("JobTread reply unreadable")
	}
	return out, nil
}

// A saved view refers to computed fields ("cfv:<id>", "cf:<id>", "pctCompleted").
// Define each as a `with` alias of the same name so the view's own where/sortBy
// run unchanged against organization.jobs.
func addAlias(with map[string]any, field []any) []any {
	if len(field) == 0 {
		return field
	}
	name, _ := field[0].(string)
	switch {
	case strings.HasPrefix(name, "cfv:") || strings.HasPrefix(name, "cf:"):
		agg := "values"
		if len(field) > 1 {
			if a, ok := field[1].(string); ok {
				agg = a
			}
		}
		e, _ := with[name].(map[string]any)
		if e == nil {
			id := name[strings.Index(name, ":")+1:]
			e = map[string]any{"_": "customFieldValues",
				"$": map[string]any{"where": []any{[]any{"customField", "id"}, "=", id}}}
			with[name] = e
		}
		e[agg] = map[string]any{"$": map[string]any{"field": "value"}}
	case name == "pctCompleted":
		// approximation of JobTread's % complete: average progress of schedule tasks
		with[name] = map[string]any{"_": "tasks",
			"$":   map[string]any{"where": []any{[]any{"isToDo"}, "=", false}},
			"avg": map[string]any{"$": map[string]any{"field": "progress"}}}
		return []any{name, "avg"}
	}
	return field
}

func rewriteFields(v any, with map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		if f, ok := t["field"].([]any); ok {
			t["field"] = addAlias(with, f)
		}
		for k, c := range t {
			if k != "field" {
				rewriteFields(c, with)
			}
		}
	case []any:
		for _, c := range t {
			rewriteFields(c, with)
		}
	}
}

func dig(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[p]
	}
	return v
}

// jtJobIDs returns the job ids of the saved view, in the view's order.
func jtJobIDs(s jtSettings) ([]string, error) {
	key, err := jtKey(s)
	if err != nil {
		return nil, err
	}
	dv, err := pave(key, map[string]any{"dataView": map[string]any{
		"$": map[string]any{"id": s.ViewID}, "options": map[string]any{}}})
	if err != nil {
		return nil, err
	}
	opts, _ := dig(dv, "dataView", "options").(map[string]any)
	if opts == nil {
		return nil, fmt.Errorf("JobTread view %s not found", s.ViewID)
	}
	with := map[string]any{}
	args := map[string]any{"size": 100, "with": with}
	if w := opts["where"]; w != nil {
		rewriteFields(w, with)
		args["where"] = w
	}
	if sb, ok := opts["sortBy"].([]any); ok && len(sb) > 0 {
		rewriteFields(sb, with)
		args["sortBy"] = sb
	}

	var ids []string
	page := ""
	for i := 0; i < 20; i++ {
		if page != "" {
			args["page"] = page
		}
		r, err := pave(key, map[string]any{"organization": map[string]any{
			"$": map[string]any{"id": s.OrgID},
			"jobs": map[string]any{"$": args, "nextPage": map[string]any{},
				"nodes": map[string]any{"id": map[string]any{}}}}})
		if err != nil {
			return nil, err
		}
		nodes, _ := dig(r, "organization", "jobs", "nodes").([]any)
		for _, n := range nodes {
			if id, _ := dig(n.(map[string]any), "id").(string); id != "" {
				ids = append(ids, id)
			}
		}
		page, _ = dig(r, "organization", "jobs", "nextPage").(string)
		if page == "" {
			break
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("the JobTread view has no jobs")
	}
	return ids, nil
}

// jtPart picks "first" (first half, rounded up), "second" (the rest) or "all".
func jtPart(ids []string, part string) []string {
	h := (len(ids) + 1) / 2
	switch strings.ToLower(part) {
	case "first", "1":
		return ids[:h]
	case "second", "2":
		return ids[h:]
	}
	return ids
}

type jtView struct{ ID, Name string }

// jtViews lists the saved Jobs views (for the Preferences dropdown).
func jtViews(s jtSettings) ([]jtView, error) {
	key, err := jtKey(s)
	if err != nil {
		return nil, err
	}
	r, err := pave(key, map[string]any{"organization": map[string]any{
		"$": map[string]any{"id": s.OrgID},
		"dataViews": map[string]any{
			"$":     map[string]any{"size": 100, "where": []any{[]any{"type"}, "=", "job"}},
			"nodes": map[string]any{"id": map[string]any{}, "name": map[string]any{}}}}})
	if err != nil {
		return nil, err
	}
	nodes, _ := dig(r, "organization", "dataViews", "nodes").([]any)
	var out []jtView
	for _, n := range nodes {
		m, _ := n.(map[string]any)
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		if id != "" {
			out = append(out, jtView{id, name})
		}
	}
	return out, nil
}
