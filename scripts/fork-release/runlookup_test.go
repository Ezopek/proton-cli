package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	rnE      = "3333333333333333333333333333333333333333"
	rnOther  = "4444444444444444444444444444444444444444"
	rnMarker = "SECRET-RUN-MARKER"
)

// rnRun renders one run the way the API does, with fields nobody reads.
func rnRun(id, attempt int, status, conclusion, head, event string, wf int) string {
	concl := "null"
	if conclusion != "" {
		concl = strconv.Quote(conclusion)
	}
	return fmt.Sprintf(`{"id":%d,"run_attempt":%d,"status":%q,"conclusion":%s,"head_sha":%q,"event":%q,"workflow_id":%d,`+
		`"name":%q,"html_url":"https://example.invalid/%s","path":".github/workflows/fork-release.yml@refs/tags/v5.1.0-ezop.1"}`,
		id, attempt, status, concl, head, event, wf, rnMarker, rnMarker)
}

func rnGoodRun(id, attempt int) string {
	return rnRun(id, attempt, "completed", "success", rnE, "push", 77)
}

// ---- the endpoint shapes ----

func TestGHArgsRunShapes(t *testing.T) {
	for endpoint, tail := range map[string]string{
		"actions/workflows/fork-release.yml": "actions/workflows/fork-release.yml",
		"actions/runs/1":                     "actions/runs/1",
		"actions/runs/1234567890123456":      "actions/runs/1234567890123456",
		"actions/runs/42/attempts/1":         "actions/runs/42/attempts/1",
		"actions/runs/42/attempts/999999":    "actions/runs/42/attempts/999999",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=100&page=1":  "actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=100&page=1",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=100&page=10": "actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=100&page=10",
	} {
		got, err := ghArgs(endpoint)
		if err != nil {
			t.Fatalf("%q: %v", endpoint, err)
		}
		want := []string{"api", "--method", "GET", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/Ezopek/proton-cli/" + tail}
		if !slices.Equal(got, want) {
			t.Fatalf("argv = %q, want %q", got, want)
		}
	}
}

func TestGHArgsRefuseOtherRunPaths(t *testing.T) {
	runs := "actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=100&page="
	for _, endpoint := range []string{
		// the workflow
		"actions/workflows", "actions/workflows/", "actions/workflows/ci.yml", "actions/workflows/fork-release.yaml",
		"actions/workflows/fork-release.yml/", "actions/workflows/fork-release.yml?x=1", "actions/workflows/Fork-Release.yml",
		"actions/workflows/fork-release.yml/enable", "actions/workflows/fork-release.yml/dispatches", "actions/workflows/1",
		"actions/workflows/fork-release.yml/timing", "actions/workflows/../fork-release.yml", "actions/workflows/fork-release.yml\n",
		// the runs of the workflow
		runs + "0", runs + "11", runs + "01", runs + "100", runs + "-1", runs + "", runs + "a", runs + "1&x=1", runs + "1\n",
		"actions/workflows/fork-release.yml/runs", "actions/workflows/fork-release.yml/runs?",
		"actions/workflows/fork-release.yml/runs?event=push&per_page=100&page=1",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&page=1&per_page=100",
		"actions/workflows/fork-release.yml/runs?head_sha=" + rnE + "&event=push&per_page=100&page=1",
		"actions/workflows/fork-release.yml/runs?event=pull_request&head_sha=" + rnE + "&per_page=100&page=1",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + strings.Repeat("A", 40) + "&per_page=100&page=1",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE[:39] + "&per_page=100&page=1",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "0&per_page=100&page=1",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=30&page=1",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=101&page=1",
		"actions/workflows/ci.yml/runs?event=push&head_sha=" + rnE + "&per_page=100&page=1",
		"actions/runs?event=push&head_sha=" + rnE + "&per_page=100&page=1",
		// a run and an attempt
		"actions/runs", "actions/runs/", "actions/runs/0", "actions/runs/01", "actions/runs/-1", "actions/runs/+1",
		"actions/runs/12345678901234567", "actions/runs/a", "actions/runs/1/", "actions/runs/1?x=1", "actions/runs/1\n",
		"actions/runs/1/attempts", "actions/runs/1/attempts/", "actions/runs/1/attempts/0", "actions/runs/1/attempts/01",
		"actions/runs/1/attempts/1234567", "actions/runs/1/attempts/-1", "actions/runs/1/attempts/1/logs",
		"actions/runs/1/logs", "actions/runs/1/rerun", "actions/runs/1/cancel", "actions/runs/1/jobs",
		"actions/runs/1/artifacts", "actions/runs/1/attempts/1/jobs", "actions/runs/../1", "actions/runs/1/../2",
		"/actions/runs/1", "repos/Other/repo/actions/runs/1", "ACTIONS/runs/1", "actions/runs/%31",
		"actions/jobs/1", "actions/artifacts/1",
	} {
		if _, err := ghArgs(endpoint); err == nil {
			t.Fatalf("%q was accepted", endpoint)
		}
	}
}

func TestExecGHRunsTheExactRunArgv(t *testing.T) {
	g, log := fakeGHAdapter(t, `{}`)
	endpoints := []string{
		"actions/workflows/fork-release.yml",
		"actions/runs/42",
		"actions/runs/42/attempts/2",
		"actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=100&page=3",
	}
	for _, e := range endpoints {
		if _, err := g.Get(e); err != nil {
			t.Fatal(err)
		}
	}
	calls := readCalls(t, log)
	if len(calls) != len(endpoints) {
		t.Fatalf("calls = %+v", calls)
	}
	for i, e := range endpoints {
		want := []string{"api", "--method", "GET", "-H", "X-GitHub-Api-Version: 2026-03-10", "repos/Ezopek/proton-cli/" + e}
		if !slices.Equal(calls[i].Argv, want) {
			t.Fatalf("call %d argv = %q", i, calls[i].Argv)
		}
	}
}

func TestExecGHRefusesARunPathBeforeRunningGH(t *testing.T) {
	g, log := fakeGHAdapter(t, `{}`)
	for _, e := range []string{"actions/runs/0", "actions/runs/1/logs", "actions/workflows/ci.yml", "actions/runs/1/attempts/01"} {
		if _, err := g.Get(e); err == nil {
			t.Fatalf("%q was run", e)
		}
	}
	if calls := readCalls(t, log); len(calls) != 0 {
		t.Fatalf("gh ran: %+v", calls)
	}
}

func TestRunEndpointBuilders(t *testing.T) {
	if got := workflowEndpoint(); got != "actions/workflows/fork-release.yml" {
		t.Fatalf("workflow endpoint %q", got)
	}
	if got := runEndpoint("42"); got != "actions/runs/42" {
		t.Fatalf("run endpoint %q", got)
	}
	if got := attemptEndpoint("42", "7"); got != "actions/runs/42/attempts/7" {
		t.Fatalf("attempt endpoint %q", got)
	}
	want := "actions/workflows/fork-release.yml/runs?event=push&head_sha=" + rnE + "&per_page=100&page=2"
	if got := runsEndpoint(rnE, 2); got != want {
		t.Fatalf("runs endpoint %q", got)
	}
	for _, e := range []string{workflowEndpoint(), runEndpoint("42"), attemptEndpoint("42", "7"), runsEndpoint(rnE, 10)} {
		if _, err := ghArgs(e); err != nil {
			t.Fatalf("builder output %q is refused: %v", e, err)
		}
	}
}

// ---- the workflow id ----

func TestLookupWorkflowID(t *testing.T) {
	good := `{"id":77,"name":"Fork Release","path":".github/workflows/fork-release.yml","state":"active"}`
	api := &fakeAPI{resp: map[string]string{"actions/workflows/fork-release.yml": good}}
	id, cause := lookupWorkflowID(api)
	if id != 77 || cause != "" {
		t.Fatalf("id %d cause %q", id, cause)
	}
	if !slices.Equal(api.calls, []string{"actions/workflows/fork-release.yml"}) {
		t.Fatalf("calls %v", api.calls)
	}
	for name, body := range map[string]string{
		"no id":           `{"name":"x"}`,
		"zero":            `{"id":0}`,
		"negative":        `{"id":-3}`,
		"fractional":      `{"id":1.5}`,
		"a string":        `{"id":"77"}`,
		"too large":       `{"id":9007199254740993}`,
		"null":            `{"id":null}`,
		"not an object":   `[77]`,
		"trailing data":   good + ` {}`,
		"not JSON":        `id=77`,
		"an empty answer": ``,
	} {
		t.Run(name, func(t *testing.T) {
			api := &fakeAPI{resp: map[string]string{"actions/workflows/fork-release.yml": body}}
			id, cause := lookupWorkflowID(api)
			if id != 0 || cause == "" {
				t.Fatalf("id %d cause %q", id, cause)
			}
		})
	}
	t.Run("a failed call", func(t *testing.T) {
		id, cause := lookupWorkflowID(&fakeAPI{resp: map[string]string{}})
		if id != 0 || cause != causeCall {
			t.Fatalf("id %d cause %q", id, cause)
		}
	})
}

// ---- a run and an attempt ----

func TestLookupRunAttempt(t *testing.T) {
	api := &fakeAPI{resp: map[string]string{"actions/runs/42/attempts/2": rnGoodRun(42, 2)}}
	r, cause := lookupRunAttempt(api, "42", "2")
	if cause != "" {
		t.Fatalf("cause %q", cause)
	}
	want := workflowRun{ID: 42, Attempt: 2, Status: "completed", Conclusion: "success", HeadSHA: rnE, Event: "push", WorkflowID: 77}
	if r != want {
		t.Fatalf("run %+v, want %+v", r, want)
	}
	if !slices.Equal(api.calls, []string{"actions/runs/42/attempts/2"}) {
		t.Fatalf("calls %v", api.calls)
	}
}

func TestLookupRun(t *testing.T) {
	api := &fakeAPI{resp: map[string]string{"actions/runs/42": rnGoodRun(42, 3)}}
	r, cause := lookupRun(api, "42")
	if cause != "" || r.ID != 42 || r.Attempt != 3 {
		t.Fatalf("run %+v cause %q", r, cause)
	}
	if !slices.Equal(api.calls, []string{"actions/runs/42"}) {
		t.Fatalf("calls %v", api.calls)
	}
}

func TestLookupRunReadsRefuse(t *testing.T) {
	good := rnGoodRun(42, 2)
	mutate := func(from, to string) string { return strings.Replace(good, from, to, 1) }
	bodies := map[string]string{
		"a failed body":         ``,
		"not an object":         `[]`,
		"another run id":        rnGoodRun(43, 2),
		"another attempt":       rnGoodRun(42, 3),
		"no id":                 mutate(`"id":42,`, ``),
		"a zero id":             mutate(`"id":42`, `"id":0`),
		"a string id":           mutate(`"id":42`, `"id":"42"`),
		"no attempt":            mutate(`"run_attempt":2,`, ``),
		"a fractional attempt":  mutate(`"run_attempt":2`, `"run_attempt":2.5`),
		"no status":             mutate(`"status":"completed",`, ``),
		"a numeric status":      mutate(`"status":"completed"`, `"status":1`),
		"an empty status":       mutate(`"status":"completed"`, `"status":""`),
		"a numeric conclusion":  mutate(`"conclusion":"success"`, `"conclusion":1`),
		"no head sha":           mutate(`"head_sha":"`+rnE+`",`, ``),
		"a short head sha":      mutate(rnE, rnE[:39]),
		"an upper case sha":     mutate(rnE, strings.ToUpper(strings.Repeat("ab", 20))),
		"no event":              mutate(`"event":"push",`, ``),
		"a numeric event":       mutate(`"event":"push"`, `"event":5`),
		"no workflow id":        mutate(`"workflow_id":77,`, ``),
		"a negative workflow":   mutate(`"workflow_id":77`, `"workflow_id":-77`),
		"trailing data":         good + ` []`,
		"an oversized id value": mutate(`"workflow_id":77`, `"workflow_id":9007199254740993`),
	}
	for name, body := range bodies {
		t.Run("attempt/"+name, func(t *testing.T) {
			r, cause := lookupRunAttempt(&fakeAPI{resp: map[string]string{"actions/runs/42/attempts/2": body}}, "42", "2")
			if cause == "" || r != (workflowRun{}) {
				t.Fatalf("run %+v cause %q", r, cause)
			}
			if strings.Contains(cause, rnMarker) || strings.Contains(cause, "http") {
				t.Fatalf("the cause quotes an answer: %q", cause)
			}
		})
	}
	// The run read is judged by the same rules, bar the attempt it was asked for.
	for name, body := range bodies {
		if name == "another attempt" {
			continue
		}
		t.Run("run/"+name, func(t *testing.T) {
			r, cause := lookupRun(&fakeAPI{resp: map[string]string{"actions/runs/42": body}}, "42")
			if cause == "" || r != (workflowRun{}) {
				t.Fatalf("run %+v cause %q", r, cause)
			}
		})
	}
	t.Run("a failed call", func(t *testing.T) {
		if _, cause := lookupRunAttempt(&fakeAPI{resp: map[string]string{}}, "42", "2"); cause != causeCall {
			t.Fatalf("cause %q", cause)
		}
		if _, cause := lookupRun(&fakeAPI{resp: map[string]string{}}, "42"); cause != causeCall {
			t.Fatalf("cause %q", cause)
		}
	})
	t.Run("a null conclusion is allowed", func(t *testing.T) {
		body := rnRun(42, 2, "in_progress", "", rnE, "push", 77)
		r, cause := lookupRunAttempt(&fakeAPI{resp: map[string]string{"actions/runs/42/attempts/2": body}}, "42", "2")
		if cause != "" || r.Conclusion != "" || r.Status != "in_progress" {
			t.Fatalf("run %+v cause %q", r, cause)
		}
	})
	t.Run("a reference that is not a decimal number makes no call", func(t *testing.T) {
		for _, c := range [][2]string{{"", "1"}, {"0", "1"}, {"042", "1"}, {"42", ""}, {"42", "0"}, {"42", "02"}, {"-4", "1"}, {"42/1", "1"}, {"42", "1/2"}, {"4 2", "1"}} {
			api := &fakeAPI{resp: map[string]string{}}
			if _, cause := lookupRunAttempt(api, c[0], c[1]); cause == "" {
				t.Fatalf("%q/%q was accepted", c[0], c[1])
			}
			if len(api.calls) != 0 {
				t.Fatalf("%q/%q made calls %v", c[0], c[1], api.calls)
			}
		}
		api := &fakeAPI{resp: map[string]string{}}
		if _, cause := lookupRun(api, "042"); cause == "" || len(api.calls) != 0 {
			t.Fatalf("cause %q calls %v", cause, api.calls)
		}
	})
}

func TestCheckAttemptFields(t *testing.T) {
	healthy := workflowRun{ID: 42, Attempt: 2, Status: "completed", Conclusion: "success", HeadSHA: rnE, Event: "push", WorkflowID: 77}
	if cause := checkAttempt(healthy, rnE, 77); cause != "" {
		t.Fatalf("the healthy attempt was refused: %q", cause)
	}
	for name, change := range map[string]func(r *workflowRun){
		"in progress":      func(r *workflowRun) { r.Status = "in_progress"; r.Conclusion = "" },
		"queued":           func(r *workflowRun) { r.Status = "queued"; r.Conclusion = "" },
		"an unknown state": func(r *workflowRun) { r.Status = "mystery" },
		"failure":          func(r *workflowRun) { r.Conclusion = "failure" },
		"cancelled":        func(r *workflowRun) { r.Conclusion = "cancelled" },
		"no conclusion":    func(r *workflowRun) { r.Conclusion = "" },
		"another commit":   func(r *workflowRun) { r.HeadSHA = rnOther },
		"another event":    func(r *workflowRun) { r.Event = "workflow_dispatch" },
		"another workflow": func(r *workflowRun) { r.WorkflowID = 78 },
	} {
		t.Run(name, func(t *testing.T) {
			r := healthy
			change(&r)
			cause := checkAttempt(r, rnE, 77)
			if cause == "" {
				t.Fatal("accepted")
			}
			if strings.Contains(cause, rnOther) {
				t.Fatalf("the cause quotes a value: %q", cause)
			}
		})
	}
	t.Run("the wanted values decide", func(t *testing.T) {
		if checkAttempt(healthy, rnOther, 77) == "" || checkAttempt(healthy, rnE, 78) == "" {
			t.Fatal("another wanted commit or workflow was accepted")
		}
	})
}

func TestRunAttemptAtLeast(t *testing.T) {
	r := workflowRun{ID: 42, Attempt: 3}
	for named, ok := range map[string]bool{"1": true, "2": true, "3": true, "4": false, "50": false} {
		cause := runAttemptAtLeast(r, named)
		if (cause == "") != ok {
			t.Fatalf("named %s: cause %q", named, cause)
		}
	}
	if runAttemptAtLeast(r, "x") == "" || runAttemptAtLeast(r, "") == "" || runAttemptAtLeast(r, "03") == "" {
		t.Fatal("a named attempt that is not a number was accepted")
	}
}

func TestRunTerminalRule(t *testing.T) {
	if !runTerminal("completed") {
		t.Fatal("completed is terminal")
	}
	for _, s := range []string{"", "queued", "in_progress", "waiting", "requested", "pending", "action_required", "success", "failure", "cancelled", "skipped", "Completed", "completed ", "mystery"} {
		if runTerminal(s) {
			t.Fatalf("%q was read as terminal", s)
		}
	}
}

// ---- the listing ----

// rnListing renders one page of the listing.
func rnListing(total int, runs ...string) string {
	return fmt.Sprintf(`{"total_count":%d,"workflow_runs":[%s]}`, total, strings.Join(runs, ","))
}

// rnPages serves a listing of n runs, all complete, 100 to a page, with the
// right total, in a fake that answers by endpoint.
func rnPages(e string, n int) *fakeAPI {
	api := &fakeAPI{resp: map[string]string{}, fail: map[string]bool{}}
	pages := (n + 99) / 100
	if pages == 0 {
		pages = 1
	}
	for p := 1; p <= pages; p++ {
		var runs []string
		for i := (p - 1) * 100; i < n && i < p*100; i++ {
			runs = append(runs, rnRun(1000+i, 1, "completed", "success", e, "push", 77))
		}
		api.resp[runsEndpoint(e, p)] = rnListing(n, runs...)
	}
	return api
}

func TestListRunsOnePage(t *testing.T) {
	api := rnPages(rnE, 2)
	l := listRuns(api, rnE)
	if l.Reason != "" || len(l.Runs) != 2 || !l.AllTerminal() {
		t.Fatalf("listing %+v", l)
	}
	if !slices.Equal(api.calls, []string{runsEndpoint(rnE, 1)}) {
		t.Fatalf("calls %v", api.calls)
	}
}

func TestListRunsEmpty(t *testing.T) {
	api := rnPages(rnE, 0)
	l := listRuns(api, rnE)
	if l.Reason != "" || len(l.Runs) != 0 || !l.AllTerminal() {
		t.Fatalf("listing %+v", l)
	}
	if len(api.calls) != 1 {
		t.Fatalf("calls %v", api.calls)
	}
}

func TestListRunsPaginates(t *testing.T) {
	for n, pages := range map[int]int{100: 1, 101: 2, 250: 3, 1000: 10} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			api := rnPages(rnE, n)
			l := listRuns(api, rnE)
			if l.Reason != "" || len(l.Runs) != n {
				t.Fatalf("reason %q, %d runs", l.Reason, len(l.Runs))
			}
			if len(api.calls) != pages {
				t.Fatalf("%d calls, want %d", len(api.calls), pages)
			}
			for i, c := range api.calls {
				if c != runsEndpoint(rnE, i+1) {
					t.Fatalf("call %d is %q", i, c)
				}
			}
		})
	}
}

func TestListRunsTerminalRule(t *testing.T) {
	mk := func(statuses ...string) *fakeAPI {
		var runs []string
		for i, s := range statuses {
			runs = append(runs, rnRun(500+i, 1, s, "", rnE, "push", 77))
		}
		return &fakeAPI{resp: map[string]string{runsEndpoint(rnE, 1): rnListing(len(runs), runs...)}}
	}
	l := listRuns(mk("completed", "completed"), rnE)
	if l.Reason != "" || !l.AllTerminal() {
		t.Fatalf("%+v", l)
	}
	for _, s := range []string{"in_progress", "queued", "waiting", "pending", "requested", "mystery"} {
		l := listRuns(mk("completed", s), rnE)
		if l.Reason != "" {
			t.Fatalf("%s: reason %q", s, l.Reason)
		}
		if l.AllTerminal() {
			t.Fatalf("%s was read as terminal", s)
		}
	}
	t.Run("a run of another tag at the commit counts", func(t *testing.T) {
		// Two tags at one commit: the other tag's attempt in progress is not terminal.
		l := listRuns(mk("completed", "in_progress", "completed"), rnE)
		if l.AllTerminal() {
			t.Fatal("a run in progress was missed")
		}
	})
}

func TestListRunsRefusals(t *testing.T) {
	ep := func(p int) string { return runsEndpoint(rnE, p) }
	run := func(i int) string { return rnRun(2000+i, 1, "completed", "success", rnE, "push", 77) }
	twoPages := func() *fakeAPI { return rnPages(rnE, 150) }
	cases := map[string]func() *fakeAPI{
		"the first page fails": func() *fakeAPI { a := twoPages(); a.fail[ep(1)] = true; return a },
		"the second page fails": func() *fakeAPI {
			a := twoPages()
			a.fail[ep(2)] = true
			return a
		},
		"the second page is missing": func() *fakeAPI { a := twoPages(); delete(a.resp, ep(2)); return a },
		"a short second page": func() *fakeAPI {
			a := twoPages()
			a.resp[ep(2)] = rnListing(150, run(1), run(2))
			return a
		},
		"an empty second page": func() *fakeAPI {
			a := twoPages()
			a.resp[ep(2)] = rnListing(150)
			return a
		},
		"a short first page": func() *fakeAPI {
			a := twoPages()
			a.resp[ep(1)] = rnListing(150, run(1))
			return a
		},
		"a total that changes": func() *fakeAPI {
			a := twoPages()
			a.resp[ep(2)] = strings.Replace(a.resp[ep(2)], `"total_count":150`, `"total_count":151`, 1)
			return a
		},
		"a page with too many entries": func() *fakeAPI {
			a := &fakeAPI{resp: map[string]string{ep(1): rnListing(1, run(1), run(2))}}
			return a
		},
		"more than the search cap": func() *fakeAPI {
			a := rnPages(rnE, 1000)
			a.resp[ep(1)] = strings.Replace(a.resp[ep(1)], `"total_count":1000`, `"total_count":1001`, 1)
			return a
		},
		"a total far past the cap": func() *fakeAPI {
			return &fakeAPI{resp: map[string]string{ep(1): rnListing(5000, run(1))}}
		},
		"no total": func() *fakeAPI { return &fakeAPI{resp: map[string]string{ep(1): `{"workflow_runs":[]}`}} },
		"a negative total": func() *fakeAPI {
			return &fakeAPI{resp: map[string]string{ep(1): `{"total_count":-1,"workflow_runs":[]}`}}
		},
		"a fractional total": func() *fakeAPI {
			return &fakeAPI{resp: map[string]string{ep(1): `{"total_count":0.5,"workflow_runs":[]}`}}
		},
		"a string total": func() *fakeAPI {
			return &fakeAPI{resp: map[string]string{ep(1): `{"total_count":"1","workflow_runs":[]}`}}
		},
		"no run list": func() *fakeAPI { return &fakeAPI{resp: map[string]string{ep(1): `{"total_count":0}`}} },
		"a null run list": func() *fakeAPI {
			return &fakeAPI{resp: map[string]string{ep(1): `{"total_count":0,"workflow_runs":null}`}}
		},
		"an array answer": func() *fakeAPI { return &fakeAPI{resp: map[string]string{ep(1): `[]`}} },
		"trailing data":   func() *fakeAPI { return &fakeAPI{resp: map[string]string{ep(1): rnListing(0) + ` {}`}} },
		"an entry that is not an object": func() *fakeAPI {
			return &fakeAPI{resp: map[string]string{ep(1): `{"total_count":1,"workflow_runs":[5]}`}}
		},
		"an entry without a status": func() *fakeAPI {
			e := strings.Replace(run(1), `"status":"completed",`, ``, 1)
			return &fakeAPI{resp: map[string]string{ep(1): rnListing(1, e)}}
		},
		"an entry for another commit": func() *fakeAPI {
			e := rnRun(9, 1, "completed", "success", rnOther, "push", 77)
			return &fakeAPI{resp: map[string]string{ep(1): rnListing(1, e)}}
		},
		"an entry of another event": func() *fakeAPI {
			e := rnRun(9, 1, "completed", "success", rnE, "workflow_dispatch", 77)
			return &fakeAPI{resp: map[string]string{ep(1): rnListing(1, e)}}
		},
		"an entry without an id": func() *fakeAPI {
			e := strings.Replace(run(1), `"id":2001,`, ``, 1)
			return &fakeAPI{resp: map[string]string{ep(1): rnListing(1, e)}}
		},
		"a repeated run": func() *fakeAPI {
			return &fakeAPI{resp: map[string]string{ep(1): rnListing(2, run(1), run(1))}}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			l := listRuns(mk(), rnE)
			if l.Reason == "" {
				t.Fatalf("accepted: %+v", l)
			}
			if l.Runs != nil {
				t.Fatalf("a refused listing still carries runs: %d", len(l.Runs))
			}
			if strings.Contains(l.Reason, rnMarker) || strings.Contains(l.Reason, "http") || strings.Contains(l.Reason, rnOther) {
				t.Fatalf("the reason quotes an answer: %q", l.Reason)
			}
		})
	}
	t.Run("the cap is judged before any further page is read", func(t *testing.T) {
		a := &fakeAPI{resp: map[string]string{ep(1): rnListing(5000, run(1))}}
		listRuns(a, rnE)
		if len(a.calls) != 1 {
			t.Fatalf("calls %v", a.calls)
		}
	})
	t.Run("a commit that is not a full SHA makes no call", func(t *testing.T) {
		a := &fakeAPI{resp: map[string]string{}}
		for _, e := range []string{"", "abc", strings.Repeat("0", 39), strings.Repeat("A", 40)} {
			if l := listRuns(a, e); l.Reason == "" {
				t.Fatalf("%q was accepted", e)
			}
		}
		if len(a.calls) != 0 {
			t.Fatalf("calls %v", a.calls)
		}
	})
	t.Run("the healthy control", func(t *testing.T) {
		if l := listRuns(twoPages(), rnE); l.Reason != "" || len(l.Runs) != 150 {
			t.Fatalf("%+v", l.Reason)
		}
	})
}

func TestListRunsFailureIsNotAnEmptyListing(t *testing.T) {
	l := listRuns(&fakeAPI{resp: map[string]string{}}, rnE)
	if l.Reason == "" {
		t.Fatal("a failed read reads as a complete listing")
	}
	var zero runListing
	if zero.Reason != "" || len(zero.Runs) != 0 {
		t.Fatal("zero value")
	}
}

func TestListRunsOverTheExecAdapter(t *testing.T) {
	// One canned page for every call: total 250 never reconciles with 2 entries.
	g, log := fakeGHAdapter(t, rnListing(250, rnGoodRun(1, 1), rnGoodRun(2, 1)))
	l := listRuns(g, rnE)
	if l.Reason == "" {
		t.Fatal("accepted")
	}
	calls := readCalls(t, log)
	if len(calls) != 1 || calls[0].Argv[5] != "repos/Ezopek/proton-cli/"+runsEndpoint(rnE, 1) {
		t.Fatalf("calls %+v", calls)
	}
}

// ---- the Run line on the release answer ----

func TestLookupReleaseCarriesTheRunLine(t *testing.T) {
	const tag = "v5.1.0-ezop.1"
	commit := "Commit: " + relE + "\n"
	for name, c := range map[string]struct {
		body  string
		value string
		valid bool
	}{
		"valid":                  {commit + "Run: 123/2\n", "123/2", true},
		"CRLF":                   {strings.ReplaceAll(commit+"Run: 123/2\n", "\n", "\r\n"), "123/2", true},
		"only the two lines":     {commit + "Run: 7/1", "7/1", true},
		"before the Commit line": {"Run: 9/9\n" + commit, "9/9", true},
		"missing":                {commit, "", false},
		"repeated":               {commit + "Run: 1/1\nRun: 1/1\n", "1/1", false},
		"two different":          {commit + "Run: 1/1\nRun: 2/1\n", "2/1", false},
		"no slash":               {commit + "Run: 11\n", "11", false},
		"a leading zero":         {commit + "Run: 01/1\n", "01/1", false},
		"a zero attempt":         {commit + "Run: 1/0\n", "1/0", false},
		"trailing text":          {commit + "Run: 1/1 tail\n", "1/1 tail", false},
		"an empty value":         {commit + "Run: \n", "", false},
		"a lower case key":       {commit + "run: 1/1\n", "", false},
		"inside a line":          {commit + "see Run: 1/1\n", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := lookupRelease(pagesOf(relPage(relEntry(tag, false, c.body))), tag, relE)
			wantRelease(t, got, releasePublishedSame)
			if got.RunValue != c.value || got.RunValid != c.valid {
				t.Fatalf("run value %q valid %v, want %q %v", got.RunValue, got.RunValid, c.value, c.valid)
			}
		})
	}
	t.Run("the other states carry no Run line", func(t *testing.T) {
		body := commit + "Run: 1/1\n"
		for name, page := range map[string]string{
			"draft": relPage(relEntry(tag, true, body)),
			"other": relPage(relEntry(tag, false, "Commit: "+relOther+"\nRun: 1/1\n")),
			"none":  relPage(relEntry("v1.0.0", false, body)),
		} {
			got := lookupRelease(pagesOf(page), tag, relE)
			if got.RunValue != "" || got.RunValid {
				t.Fatalf("%s carries a Run line: %+v", name, got)
			}
		}
		got := lookupRelease(&fakePages{pages: map[int]string{}, fail: map[int]bool{1: true}}, tag, relE)
		if got.State != releaseUnknown || got.RunValue != "" || got.RunValid {
			t.Fatalf("%+v", got)
		}
	})
}
