package config

import "testing"

// TestRealConfigFilesValidate loads the shipped config files (the documented
// agent.config.yaml and the fast e2e config) and validates them: a committed
// config must stay loadable. Credentials and the model are filled
// programmatically — the files deliberately carry neither.
func TestRealConfigFilesValidate(t *testing.T) {
	for _, p := range []string{"../../agent.config.yaml", "../../testdata/config/agent.e2e.yaml"} {
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("%s: load: %v", p, err)
		}
		cfg.LLM.APIKey = "test-key"
		cfg.LLM.Model = "test-model"
		if err := Validate(cfg); err != nil {
			t.Fatalf("%s: validate: %v", p, err)
		}
		t.Logf("%s: loop=%+v", p, cfg.Loop)
	}
}
