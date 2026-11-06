package main

import (
	"math"
	"strconv"
)

// The run reads answer what the Actions API says about the runs of
// fork-release.yml: the id of the workflow by its file name, one run attempt,
// one run, and the listing of the runs of the workflow for one commit and the
// push event. They decide nothing about a release: verify and verify --status
// turn the answers into a record or a status. Every doubt is a refusal, because
// the documentation does not describe the run fields of a tag-push run or the
// status of a run during a re-run.

const (
	// runsPerPage is the page size asked for: the documented maximum.
	runsPerPage = 100
	// runsSearchCap is the most results the API returns under the filters used.
	// A total above it means the listing is cut short.
	runsSearchCap = 1000
	// runsPageCeiling is the most pages a listing under the cap can have.
	runsPageCeiling = runsSearchCap / runsPerPage
)

// Reasons of a refused run read, beside the shared causes. They are one closed
// set of words and never quote an answer.
const (
	causeBadRunRef      = "not a run reference"
	causeNoID           = "no usable id"
	causeNoAttempt      = "no usable run attempt"
	causeNoStatus       = "no usable status"
	causeNoConclusion   = "no usable conclusion"
	causeNoHeadSHA      = "no usable head SHA"
	causeNoEvent        = "no usable event"
	causeNoWorkflowID   = "no usable workflow id"
	causeNoTotal        = "no usable total"
	causeNoRuns         = "no run list"
	causeNotCompleted   = "not completed"
	causeNotSuccess     = "not a success"
	causeOtherCommit    = "another commit"
	causeOtherEvent     = "another event"
	causeOtherWorkflow  = "another workflow"
	causeAttemptBelow   = "attempt below the named one"
	causeShortPage      = "short page"
	causeCount          = "count does not reconcile"
	causeCap            = "over the search cap"
	causeOutsideFilters = "entry outside the filters"
)

// workflowEndpoint is the read of the workflow of fork-release.yml by file name.
func workflowEndpoint() string { return "actions/workflows/" + forkRelease }

// runEndpoint is the read of one run by id.
func runEndpoint(id string) string { return "actions/runs/" + id }

// attemptEndpoint is the read of one run attempt by run id and attempt number.
func attemptEndpoint(id, attempt string) string {
	return "actions/runs/" + id + "/attempts/" + attempt
}

// runsEndpoint is page of the listing of the runs of fork-release.yml for the
// push event and the head commit e.
func runsEndpoint(e string, page int) string {
	return "actions/workflows/" + forkRelease + "/runs?event=push&head_sha=" + e +
		"&per_page=" + strconv.Itoa(runsPerPage) + "&page=" + strconv.Itoa(page)
}

// workflowRun is the part of a run (or of a run attempt) the helper reads.
// Conclusion is empty while the run has none.
type workflowRun struct {
	ID         int64
	Attempt    int64
	Status     string
	Conclusion string
	HeadSHA    string
	Event      string
	WorkflowID int64
}

// wholeNumber reads a JSON number that is a whole number from 1 up to the
// largest that survives a float64.
func wholeNumber(v any) (int64, bool) {
	num, ok := v.(float64)
	if !ok || num != math.Trunc(num) || num < 1 || num > maxAssetID {
		return 0, false
	}
	return int64(num), true
}

// parseRun reads one run object. The id, the status, the head SHA and the event
// are always required; the run attempt and the workflow id are required when
// full is set, which is for the single reads. The conclusion may be null or
// missing, and when present it is text. The answer is a cause, empty when the
// object reads.
func parseRun(m map[string]any, full bool) (workflowRun, string) {
	var r workflowRun
	var ok bool
	if r.ID, ok = wholeNumber(m["id"]); !ok {
		return workflowRun{}, causeNoID
	}
	if r.Status, ok = m["status"].(string); !ok || r.Status == "" {
		return workflowRun{}, causeNoStatus
	}
	switch c := m["conclusion"].(type) {
	case nil:
	case string:
		r.Conclusion = c
	default:
		return workflowRun{}, causeNoConclusion
	}
	if r.HeadSHA, ok = m["head_sha"].(string); !ok || !fullSHA.MatchString(r.HeadSHA) {
		return workflowRun{}, causeNoHeadSHA
	}
	if r.Event, ok = m["event"].(string); !ok || r.Event == "" {
		return workflowRun{}, causeNoEvent
	}
	if full {
		if r.Attempt, ok = wholeNumber(m["run_attempt"]); !ok {
			return workflowRun{}, causeNoAttempt
		}
		if r.WorkflowID, ok = wholeNumber(m["workflow_id"]); !ok {
			return workflowRun{}, causeNoWorkflowID
		}
	}
	return r, ""
}

// lookupWorkflowID reads the workflow of fork-release.yml by file name and
// answers its id, or a cause.
func lookupWorkflowID(g ghGetter) (int64, string) {
	var m map[string]any
	if cause := getJSON(g, workflowEndpoint(), &m); cause != "" {
		return 0, cause
	}
	id, ok := wholeNumber(m["id"])
	if !ok {
		return 0, causeNoID
	}
	return id, ""
}

// readRunObject reads one run object from an endpoint.
func readRunObject(g ghGetter, endpoint string) (workflowRun, string) {
	var m map[string]any
	if cause := getJSON(g, endpoint, &m); cause != "" {
		return workflowRun{}, cause
	}
	return parseRun(m, true)
}

// lookupRunAttempt reads one attempt of a run. runID and attempt are the two
// numbers of a Run line. The answer must be that run and that attempt.
func lookupRunAttempt(g ghGetter, runID, attempt string) (workflowRun, string) {
	if !positiveInt.MatchString(runID) || !positiveInt.MatchString(attempt) {
		return workflowRun{}, causeBadRunRef
	}
	r, cause := readRunObject(g, attemptEndpoint(runID, attempt))
	if cause != "" {
		return workflowRun{}, cause
	}
	if strconv.FormatInt(r.ID, 10) != runID || strconv.FormatInt(r.Attempt, 10) != attempt {
		return workflowRun{}, causeMismatch
	}
	return r, ""
}

// lookupRun reads one run by id. The answer must be that run.
func lookupRun(g ghGetter, runID string) (workflowRun, string) {
	if !positiveInt.MatchString(runID) {
		return workflowRun{}, causeBadRunRef
	}
	r, cause := readRunObject(g, runEndpoint(runID))
	if cause != "" {
		return workflowRun{}, cause
	}
	if strconv.FormatInt(r.ID, 10) != runID {
		return workflowRun{}, causeMismatch
	}
	return r, ""
}

// runTerminal is the terminal rule: a run is over only when its status is
// completed. The API documents statuses and conclusions in one vocabulary, so
// anything else, an unknown value included, counts as not over.
func runTerminal(status string) bool { return status == "completed" }

// checkAttempt requires a run attempt to be a finished success of the push event
// for the commit e, in the workflow wf. The answer is a cause, empty when it is.
func checkAttempt(r workflowRun, e string, wf int64) string {
	switch {
	case !runTerminal(r.Status):
		return causeNotCompleted
	case r.Conclusion != runConclusionOK:
		return causeNotSuccess
	case r.HeadSHA != e:
		return causeOtherCommit
	case r.Event != "push":
		return causeOtherEvent
	case r.WorkflowID != wf:
		return causeOtherWorkflow
	}
	return ""
}

// runAttemptAtLeast requires the run's current attempt to be at least the named
// one (the digits of a Run line). The answer is a cause, empty when it is.
func runAttemptAtLeast(r workflowRun, named string) string {
	if !positiveInt.MatchString(named) {
		return causeBadRunRef
	}
	n, err := strconv.ParseInt(named, 10, 64)
	if err != nil {
		return causeBadRunRef
	}
	if r.Attempt < n {
		return causeAttemptBelow
	}
	return ""
}

// runListing is the answer of listRuns. Reason is empty only for a listing that
// was read to its end and reconciles with the total the API gave; then Runs is
// every run. A refused listing carries no runs.
type runListing struct {
	Runs   []workflowRun
	Reason string
}

// AllTerminal reports whether every run of the listing is over. It is not a
// statement about a listing that was refused.
func (l runListing) AllTerminal() bool {
	for _, r := range l.Runs {
		if !runTerminal(r.Status) {
			return false
		}
	}
	return true
}

// listRuns lists the runs of fork-release.yml for the push event and the commit
// e, 100 a page, to the end. The first page gives the total, which must not pass
// the search cap; every page must hold exactly the entries the total says it
// should, the total must not change, and the count of runs read must end equal
// to it. A failed, short or unreconciled page refuses the whole listing.
func listRuns(g ghGetter, e string) runListing {
	refused := func(step, cause string) runListing { return runListing{Reason: step + cause} }
	if !fullSHA.MatchString(e) {
		return refused("commit: ", causeNotFullSHA)
	}
	var runs []workflowRun
	seen := map[int64]bool{}
	total := -1
	for page := 1; ; page++ {
		step := "runs page " + strconv.Itoa(page) + ": "
		if page > runsPageCeiling {
			return refused(step, causeCap)
		}
		var m map[string]any
		if cause := getJSON(g, runsEndpoint(e, page), &m); cause != "" {
			return refused(step, cause)
		}
		count, ok := m["total_count"].(float64)
		if !ok || count != math.Trunc(count) || count < 0 || count > 1<<31 {
			return refused(step, causeNoTotal)
		}
		if total >= 0 && int(count) != total {
			return refused(step, causeCount)
		}
		total = int(count)
		if total > runsSearchCap {
			return refused(step, causeCap)
		}
		entries, ok := m["workflow_runs"].([]any)
		if !ok {
			return refused(step, causeNoRuns)
		}
		want := min(runsPerPage, total-len(runs))
		switch {
		case len(entries) < want:
			return refused(step, causeShortPage)
		case len(entries) > want:
			return refused(step, causeCount)
		}
		for _, item := range entries {
			obj, ok := item.(map[string]any)
			if !ok {
				return refused(step, causeMalformed)
			}
			r, cause := parseRun(obj, false)
			if cause != "" {
				return refused(step, cause)
			}
			if r.HeadSHA != e || r.Event != "push" {
				return refused(step, causeOutsideFilters)
			}
			if seen[r.ID] {
				return refused(step, causeRepeated)
			}
			seen[r.ID] = true
			runs = append(runs, r)
		}
		if len(runs) == total {
			return runListing{Runs: runs}
		}
	}
}
