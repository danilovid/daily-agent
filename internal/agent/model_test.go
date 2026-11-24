package agent

import "testing"

func TestNormalizeModelURL(t *testing.T) {
	ok := map[string]string{
		"http://127.0.0.1:8000/v1/":           "http://127.0.0.1:8000/v1",
		" https://llm.example.test/v1 ":       "https://llm.example.test/v1",
		"http://localhost:11434/v1":           "http://localhost:11434/v1",
		"https://models.internal.test":        "https://models.internal.test",
		"https://gpu-01.lab.example.test/api": "https://gpu-01.lab.example.test/api",
	}
	for in, want := range ok {
		got, err := NormalizeModelURL(in)
		if err != nil || got != want {
			t.Errorf("%q → %q, %v; ждали %q", in, got, err, want)
		}
	}
	bad := []string{
		"",
		"llm.example.test/v1",
		"ftp://llm.example.test",
		"https://user:secret@llm.example.test/v1",
		"https://llm.example.test/v1?key=1",
		"https://llm.example.test/v1#x",
		"https://api.openai.com/v1",
		"https://api.anthropic.com/v1",
		"https://openrouter.ai/api/v1",
		"https://my-org.openai.azure.com/openai",
		"https://generativelanguage.googleapis.com/v1beta/openai",
		"https://API.DEEPSEEK.COM/v1",
		"http://127.0.0.1:8765/v1https://api.openai.com/v1",
		"http://127.0.0.1:8765//v1",
	}
	for _, in := range bad {
		if got, err := NormalizeModelURL(in); err == nil {
			t.Errorf("%q прошёл проверку: %q", in, got)
		}
	}
}

func TestIsCloudHost(t *testing.T) {
	for _, h := range []string{"api.openai.com", "openai.com", "api.mistral.ai", "api.groq.com.", "bedrock-runtime.us-east-1.amazonaws.com"} {
		if !IsCloudHost(h) {
			t.Errorf("%s должен считаться облаком", h)
		}
	}
	for _, h := range []string{"127.0.0.1", "localhost", "llm.example.test", "notopenai.com", "openai.com.example.test", "::1"} {
		if IsCloudHost(h) {
			t.Errorf("%s не облако", h)
		}
	}
}

func TestValidateModelID(t *testing.T) {
	for _, id := range []string{"test-model", "org/model-7b:q4", "model.v2"} {
		if err := ValidateModelID(id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	for _, id := range []string{"", "  ", "with space", "tab\tid"} {
		if err := ValidateModelID(id); err == nil {
			t.Errorf("%q прошёл проверку", id)
		}
	}
}
