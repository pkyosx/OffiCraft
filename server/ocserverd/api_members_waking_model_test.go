package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func reportWaking(t *testing.T, api *apiServer, sub, model string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReportWakingApiSelfWakingPost(rec,
		taskReq(t, http.MethodPost, "/api/self/waking", map[string]string{"model": model}, sub, "agent"))
	return rec
}

func TestReportWakingMemberStoresReportedModelSeparately(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "wake-member", Kind: KindStaff,
		Model: "owner-selected", DesiredState: DesiredStateOnline,
		RefocusSince: 1, StoppingSince: 2, StoppedSince: 3})

	rec := reportWaking(t, api, "wake-member", "caller-supplied")
	if rec.Code != http.StatusOK {
		t.Fatalf("report_waking: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, err := dal.GetMember("wake-member")
	if err != nil || m == nil {
		t.Fatalf("reload member: %v", err)
	}
	if m.Model != "owner-selected" {
		t.Fatalf("report_waking overwrote owner model: got %q", m.Model)
	}
	if m.ActualModel != "caller-supplied" {
		t.Fatalf("actual_model = %q, want caller-supplied", m.ActualModel)
	}
	if m.WakingSince == 0 || m.RefocusSince != 0 || m.StoppingSince != 0 || m.StoppedSince != 0 {
		t.Fatalf("report_waking must still stamp waking and clear recycle markers: %+v", *m)
	}
}

func TestReportWakingOutsourceStoresReportedModelSeparately(t *testing.T) {
	api, dal := newGateTestAPI(t)
	if err := dal.PutOutsourceWorker(OutsourceWorker{ID: "ow-wake-model", Codename: "X-wake-model",
		Model: "owner-selected", Effort: "medium", TaskID: "t-wake-model",
		Status: WorkerStatusActive, DesiredState: DesiredStateOnline,
		RefocusSince: 1, StoppingSince: 2, StoppedSince: 3}); err != nil {
		t.Fatalf("put worker: %v", err)
	}

	rec := reportWaking(t, api, "ow-wake-model", "caller-supplied")
	if rec.Code != http.StatusOK {
		t.Fatalf("report_waking: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	w, err := dal.GetOutsourceWorker("ow-wake-model")
	if err != nil || w == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if w.Model != "owner-selected" {
		t.Fatalf("report_waking overwrote owner model: got %q", w.Model)
	}
	if w.ActualModel != "caller-supplied" {
		t.Fatalf("actual_model = %q, want caller-supplied", w.ActualModel)
	}
	if w.RefocusSince != 0 || w.StoppingSince != 0 || w.StoppedSince != 0 {
		t.Fatalf("report_waking must still clear recycle markers: %+v", *w)
	}
}
