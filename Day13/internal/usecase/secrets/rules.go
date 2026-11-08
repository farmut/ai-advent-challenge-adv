package secrets

import "regexp"

// rule is one detector. Group selects the submatch that holds the secret (0
// is the whole match). Validate, when set, filters candidates. A smaller
// Priority wins when spans of the same length overlap.
type rule struct {
	Type     Type
	Re       *regexp.Regexp
	Group    int
	Validate func(string) bool
	Priority int
}

var rules = []rule{
	{Type: PrivateKey, Priority: 0, Re: regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`)},
	{Type: APIKeyAnthropic, Priority: 1, Re: regexp.MustCompile(`\bsk-ant-(?:api\d\d|admin\d\d)-[A-Za-z0-9_-]{20,}`)},
	// The sk-proj-/sk-svcacct-/sk-admin- prefixes are unique enough to allow
	// a short tail ("sk-proj-abc").
	{Type: APIKeyOpenAI, Priority: 2, Re: regexp.MustCompile(`\bsk-(?:proj|svcacct|admin)-[A-Za-z0-9_-]{3,}`)},
	{Type: APIKeyOpenAI, Priority: 2, Re: regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`)},
	{Type: GitHubToken, Priority: 3, Re: regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}\b`)},
	{Type: GitHubToken, Priority: 3, Re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}`)},
	{Type: AWSAccessKey, Priority: 4, Re: regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`)},
	{Type: AWSSecretKey, Priority: 5, Group: 1, Re: regexp.MustCompile(`(?i)(?:aws_?secret(?:_access)?_?key|secret[_ ]?access[_ ]?key)["']?\s*[:=]\s*["']?([A-Za-z0-9/+=]{40})\b`)},
	{Type: SlackToken, Priority: 6, Re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{Type: GoogleAPIKey, Priority: 7, Re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{Type: JWT, Priority: 8, Re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
	{Type: Card, Priority: 9, Validate: validCard, Re: regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)},
	{Type: Email, Priority: 10, Re: regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)*\.[a-z]{2,}\b`)},
	{Type: Phone, Priority: 11, Re: regexp.MustCompile(`(?:\+7|\b8)[\s(-]*\d{3}[\s)-]*\d{3}[\s-]*\d{2}[\s-]*\d{2}\b`)},
	{Type: Phone, Priority: 11, Validate: validE164, Re: regexp.MustCompile(`\+[1-9][\d\s().-]{8,18}\d`)},
}

var priorities = func() map[Type]int {
	m := map[Type]int{}
	for _, r := range rules {
		m[r.Type] = r.Priority
	}
	return m
}()

// priority returns the rule priority of t (unknown types lose).
func priority(t Type) int {
	if p, ok := priorities[t]; ok {
		return p
	}
	return 1 << 20
}
