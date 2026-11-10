package domain

import (
	"fmt"
	"strings"
	"testing"
)

var knownProfiles = map[string]bool{"coder": true, "researcher": true}

// validPlan returns the smallest plan that passes Validate.
func validPlan() PlanV1 {
	return PlanV1{
		Schema: "plan.v1",
		Subtasks: []PlanItem{{
			ID:         "a",
			Title:      "подзадача A",
			Spec:       "сделать A",
			Profile:    "coder",
			Acceptance: []string{"A работает"},
			Root:       0,
		}},
	}
}

func TestPlanV1_Validate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*PlanV1)
		roots   int
		maxN    int
		wantErr string
	}{
		{"валидный минимум", nil, 1, 20, ""},
		{"неверная schema", func(p *PlanV1) { p.Schema = "plan.v2" }, 1, 20, "schema"},
		{"ноль подзадач", func(p *PlanV1) { p.Subtasks = nil }, 1, 20, "диапазона"},
		{"21 подзадача при maxN=20", func(p *PlanV1) {
			p.Subtasks = make([]PlanItem, 21)
			for i := range p.Subtasks {
				p.Subtasks[i] = PlanItem{
					ID: fmt.Sprintf("st-%d", i), Title: "t", Spec: "s",
					Profile: "coder", Acceptance: []string{"a"}, Root: 0,
				}
			}
		}, 1, 20, "диапазона"},
		{"maxN выше потолка 20", func(p *PlanV1) {
			for range 21 {
				i := len(p.Subtasks)
				p.Subtasks = append(p.Subtasks, PlanItem{
					ID: fmt.Sprintf("st-%d", i), Title: "t", Spec: "s",
					Profile: "coder", Acceptance: []string{"a"}, Root: 0,
				})
			}
		}, 1, 25, "диапазона"},
		{"дубль id", func(p *PlanV1) {
			p.Subtasks = append(p.Subtasks, p.Subtasks[0])
		}, 1, 20, "дубль"},
		{"id вне регэкспа", func(p *PlanV1) { p.Subtasks[0].ID = "Bad_ID" }, 1, 20, "id"},
		{"ссылка на несуществующую", func(p *PlanV1) {
			p.Subtasks[0].DependsOn = []string{"ghost"}
		}, 1, 20, "несуществующую"},
		{"ссылка на себя", func(p *PlanV1) {
			p.Subtasks = append(p.Subtasks, PlanItem{
				ID: "b", Title: "t", Spec: "s", Profile: "coder",
				Acceptance: []string{"a"}, Root: 0, DependsOn: []string{"b"},
			})
		}, 1, 20, "сама на себя"},
		{"цикл в графе", func(p *PlanV1) {
			p.Subtasks = append(p.Subtasks,
				PlanItem{ID: "b", Title: "t", Spec: "s", Profile: "coder", Acceptance: []string{"a"}, Root: 0, DependsOn: []string{"c"}},
				PlanItem{ID: "c", Title: "t", Spec: "s", Profile: "coder", Acceptance: []string{"a"}, Root: 0, DependsOn: []string{"b"}},
			)
		}, 1, 20, "циклическая"},
		{"неизвестный профиль", func(p *PlanV1) { p.Subtasks[0].Profile = "wizard" }, 1, 20, "профиль"},
		{"пустой title", func(p *PlanV1) { p.Subtasks[0].Title = "" }, 1, 20, "title"},
		{"пустой spec", func(p *PlanV1) { p.Subtasks[0].Spec = "" }, 1, 20, "spec"},
		{"пустой acceptance", func(p *PlanV1) { p.Subtasks[0].Acceptance = nil }, 1, 20, "acceptance"},
		{"пустой пункт acceptance", func(p *PlanV1) { p.Subtasks[0].Acceptance = []string{"ok", ""} }, 1, 20, "acceptance"},
		{"root вне диапазона", func(p *PlanV1) { p.Subtasks[0].Root = 1 }, 1, 20, "root"},
		{"непокрытый root", func(p *PlanV1) {
			p.Subtasks = append(p.Subtasks, PlanItem{
				ID: "b", Title: "t", Spec: "s", Profile: "coder",
				Acceptance: []string{"a"}, Root: 0,
			})
		}, 2, 20, "не покрыт"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roots, maxN := tc.roots, tc.maxN
			if roots == 0 {
				roots = 1
			}
			p := validPlan()
			if tc.mutate != nil {
				tc.mutate(&p)
			}
			err := p.Validate(knownProfiles, roots, maxN)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want ошибку содержащую %q", err, tc.wantErr)
			}
		})
	}
}

func TestTopoOrder_Happy(t *testing.T) {
	items := []PlanItem{
		{ID: "c", DependsOn: []string{"b"}},
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
	}
	order, err := TopoOrder(items)
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("TopoOrder len = %d, want 3", len(order))
	}
	pos := make(map[string]int, len(order))
	for i, id := range order {
		pos[id] = i
	}
	for _, dep := range []struct{ before, after string }{{"a", "b"}, {"b", "c"}} {
		if pos[dep.before] >= pos[dep.after] {
			t.Errorf("TopoOrder %v: %s должен идти раньше %s", order, dep.before, dep.after)
		}
	}
}

func TestTopoOrder_Errors(t *testing.T) {
	cases := []struct {
		name    string
		items   []PlanItem
		wantErr string
	}{
		{"цикл самоссылка", []PlanItem{{ID: "a", DependsOn: []string{"a"}}}, "циклическая"},
		{"цикл длины 2", []PlanItem{
			{ID: "a", DependsOn: []string{"b"}},
			{ID: "b", DependsOn: []string{"a"}},
		}, "циклическая"},
		{"несуществующая зависимость", []PlanItem{{ID: "a", DependsOn: []string{"ghost"}}}, "несуществующую"},
		{"дубль id", []PlanItem{{ID: "a"}, {ID: "a"}}, "дубль"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TopoOrder(tc.items)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("TopoOrder() = %v, want ошибку содержащую %q", err, tc.wantErr)
			}
		})
	}
}
