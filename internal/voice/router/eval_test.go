//go:build voiceeval

package router

// The router's evals on a real model (evals/voice-router/cases.json): every case is planned by the plan's lightest
// model through the Codex CLI signed in with the person's ChatGPT plan, and judged by its actions. Not part of `go
// test ./...`: it asks a model. VOICE_EVAL_MODEL picks another model, VOICE_EVAL_REPEAT runs each case N times,
// VOICE_EVAL_ONLY runs the cases whose id contains it, VOICE_EVAL_MIN is the pass share that fails the run (0.9).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type evalFile struct {
	Screen  string     `json:"screen"`
	Chats   []Chat     `json:"chats"`
	Catalog Catalog    `json:"catalog"`
	Cases   []evalCase `json:"cases"`
}

type evalCase struct {
	ID           string              `json:"id"`
	Task         string              `json:"task"`
	Screen       string              `json:"screen"`
	ChatsCurrent string              `json:"chats_current"`
	Need         []map[string]string `json:"need"`
	Not          []map[string]string `json:"not"`
	Say          string              `json:"say"`
	NoActions    bool                `json:"no_actions"`
}

func actionFields(a Action) map[string]string {
	raw, _ := json.Marshal(a)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	out := map[string]string{}
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = x
		case float64:
			out[k] = strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(x)
		}
	}
	return out
}

func matches(a Action, want map[string]string) bool {
	have := actionFields(a)
	if have["do"] != want["do"] {
		return false
	}
	for k, re := range want {
		if k == "do" {
			continue
		}
		if !regexp.MustCompile(re).MatchString(have[k]) {
			return false
		}
	}
	return true
}

func judge(c evalCase, p Plan) []string {
	var fails []string
	for _, w := range c.Need {
		ok := false
		for _, a := range p.Actions {
			if matches(a, w) {
				ok = true
			}
		}
		if !ok {
			fails = append(fails, fmt.Sprintf("needs %v", w))
		}
	}
	for _, w := range c.Not {
		for _, a := range p.Actions {
			if matches(a, w) {
				fails = append(fails, fmt.Sprintf("must not %v", w))
			}
		}
	}
	if c.Say != "" && !regexp.MustCompile(c.Say).MatchString(p.Say) {
		fails = append(fails, "say does not match "+c.Say)
	}
	if c.NoActions && len(p.Actions) > 0 {
		fails = append(fails, "wants no actions")
	}
	if strings.TrimSpace(p.Say) == "" {
		fails = append(fails, "empty say")
	}
	return fails
}

func TestVoiceRouterEvals(t *testing.T) {
	raw, err := os.ReadFile("../../../evals/voice-router/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var f evalFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	repeat, _ := strconv.Atoi(os.Getenv("VOICE_EVAL_REPEAT"))
	if repeat < 1 {
		repeat = 1
	}
	min := 0.9
	if v, err := strconv.ParseFloat(os.Getenv("VOICE_EVAL_MIN"), 64); err == nil {
		min = v
	}
	only := os.Getenv("VOICE_EVAL_ONLY")
	r := &Router{
		Ask: CodexAsker("", os.Getenv("VOICE_EVAL_MODEL"), os.Getenv("VOICE_EVAL_EFFORT")),
		Recall: func(_ context.Context, q string) []string {
			return []string{"Чат «Тарифы и цены», вчера 18:40: решили три тарифа — бесплатный, Про за 20$, Команда за 50$; " +
				"осталось решить годовую скидку."}
		},
		Timeout: 90 * time.Second,
	}
	type result struct {
		id    string
		fails []string
		ms    int64
		plan  Plan
	}
	var (
		mu      sync.Mutex
		results []result
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 4)
	)
	for _, c := range f.Cases {
		if only != "" && !strings.Contains(c.ID, only) {
			continue
		}
		for i := 0; i < repeat; i++ {
			wg.Add(1)
			go func(c evalCase) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				req := Request{Task: c.Task, Screen: f.Screen, Chats: append([]Chat(nil), f.Chats...), Catalog: f.Catalog}
				if c.Screen != "" {
					req.Screen = c.Screen
				}
				if c.ChatsCurrent == "none" {
					for j := range req.Chats {
						req.Chats[j].Current = false
					}
				}
				p, err := r.Route(context.Background(), req)
				res := result{id: c.ID, ms: p.Ms, plan: p}
				if err != nil {
					res.fails = []string{"error: " + err.Error()}
				} else {
					res.fails = judge(c, p)
				}
				mu.Lock()
				results = append(results, res)
				mu.Unlock()
			}(c)
		}
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].id < results[j].id })
	pass := 0
	var total int64
	for _, res := range results {
		total += res.ms
		plan, _ := json.Marshal(res.plan.Actions)
		if len(res.fails) == 0 {
			pass++
			t.Logf("PASS %-24s %5dms say=%q actions=%s", res.id, res.ms, res.plan.Say, plan)
			continue
		}
		t.Logf("FAIL %-24s %5dms %s | say=%q actions=%s", res.id, res.ms, strings.Join(res.fails, "; "), res.plan.Say, plan)
	}
	if len(results) == 0 {
		t.Fatal("no cases ran")
	}
	share := float64(pass) / float64(len(results))
	t.Logf("passed %d/%d (%.0f%%), mean %dms", pass, len(results), share*100, total/int64(len(results)))
	if share < min {
		t.Fatalf("pass share %.2f below %.2f", share, min)
	}
}
