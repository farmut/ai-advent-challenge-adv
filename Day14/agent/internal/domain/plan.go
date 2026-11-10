package domain

import (
	"fmt"
	"regexp"
)

// PlanV1 is the strict JSON contract the planner LLM must answer with.
// DecodeStrict + Validate are the only path from the model's text to a plan.
type PlanV1 struct {
	Schema   string     `json:"schema"`
	Subtasks []PlanItem `json:"subtasks"`
}

// PlanItem is one subtask of a plan, before it becomes a LoopRun subtask.
type PlanItem struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Spec       string   `json:"spec"`
	Profile    string   `json:"profile"`
	Stack      []string `json:"stack,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
	Acceptance []string `json:"acceptance"`
	Root       int      `json:"root"`
}

// planSchemaVersion is the only schema a plan may claim. Anything else is
// rejected instead of being guessed at.
const planSchemaVersion = "plan.v1"

// planMaxSubtasks is the hard ceiling of the plan format itself, regardless
// of the (lower) per-run configured limit passed to Validate as maxN.
const planMaxSubtasks = 20

var planIDPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Validate checks the plan against its contract. known is the set of profile
// names the planner was told about; roots is the number of goal bullet points
// every subtask must map to (and collectively cover); maxN is the per-run
// subtask limit, never above planMaxSubtasks.
func (p PlanV1) Validate(known map[string]bool, roots, maxN int) error {
	if p.Schema != planSchemaVersion {
		return fmt.Errorf("план: schema должна быть %q, получено %q", planSchemaVersion, p.Schema)
	}
	if maxN < 1 {
		return fmt.Errorf("план: maxN должен быть >= 1, получено %d", maxN)
	}
	if limit := min(maxN, planMaxSubtasks); len(p.Subtasks) < 1 || len(p.Subtasks) > limit {
		return fmt.Errorf("план: число подзадач %d вне диапазона [1, %d]", len(p.Subtasks), limit)
	}
	if roots < 1 {
		return fmt.Errorf("план: число пунктов цели должно быть >= 1, получено %d", roots)
	}
	seen := make(map[string]bool, len(p.Subtasks))
	for _, it := range p.Subtasks {
		if !planIDPattern.MatchString(it.ID) {
			return fmt.Errorf("план: невалидный id подзадачи %q (нужен %s)", it.ID, planIDPattern)
		}
		if seen[it.ID] {
			return fmt.Errorf("план: дубль id подзадачи %q", it.ID)
		}
		seen[it.ID] = true
	}
	for _, it := range p.Subtasks {
		for _, dep := range it.DependsOn {
			if dep == it.ID {
				return fmt.Errorf("план: подзадача %s ссылается сама на себя", it.ID)
			}
			if !seen[dep] {
				return fmt.Errorf("план: подзадача %s ссылается на несуществующую %q", it.ID, dep)
			}
		}
		if !known[it.Profile] {
			return fmt.Errorf("план: неизвестный профиль %q у подзадачи %s", it.Profile, it.ID)
		}
		if it.Title == "" {
			return fmt.Errorf("план: пустой title у подзадачи %s", it.ID)
		}
		if it.Spec == "" {
			return fmt.Errorf("план: пустой spec у подзадачи %s", it.ID)
		}
		if len(it.Acceptance) == 0 {
			return fmt.Errorf("план: пустой acceptance у подзадачи %s", it.ID)
		}
		for _, a := range it.Acceptance {
			if a == "" {
				return fmt.Errorf("план: пустой пункт acceptance у подзадачи %s", it.ID)
			}
		}
		if it.Root < 0 || it.Root >= roots {
			return fmt.Errorf("план: root %d у подзадачи %s вне диапазона [0, %d)", it.Root, it.ID, roots)
		}
	}
	if _, err := TopoOrder(p.Subtasks); err != nil {
		return err
	}
	covered := make([]bool, roots)
	for _, it := range p.Subtasks {
		covered[it.Root] = true
	}
	for i, ok := range covered {
		if !ok {
			return fmt.Errorf("план: пункт цели %d не покрыт ни одной подзадачей", i)
		}
	}
	return nil
}

// TopoOrder returns the subtask ids in dependency order (every dependency
// before its dependents) using Kahn's algorithm. It fails on duplicate ids,
// references to unknown ids and cycles — a plan that does not order is not a
// plan the loop can execute.
func TopoOrder(items []PlanItem) ([]string, error) {
	byID := make(map[string]*PlanItem, len(items))
	for i := range items {
		if _, dup := byID[items[i].ID]; dup {
			return nil, fmt.Errorf("план: дубль id подзадачи %q", items[i].ID)
		}
		byID[items[i].ID] = &items[i]
	}
	indegree := make(map[string]int, len(items))
	dependents := make(map[string][]string, len(items))
	for i := range items {
		indegree[items[i].ID] += 0
		for _, dep := range items[i].DependsOn {
			if _, ok := byID[dep]; !ok {
				return nil, fmt.Errorf("план: подзадача %s ссылается на несуществующую %q", items[i].ID, dep)
			}
			indegree[items[i].ID]++
			dependents[dep] = append(dependents[dep], items[i].ID)
		}
	}
	queue := make([]string, 0, len(items))
	for id, d := range indegree {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	order := make([]string, 0, len(items))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, next := range dependents[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(order) != len(items) {
		return nil, fmt.Errorf("план: циклическая зависимость между подзадачами")
	}
	return order, nil
}
