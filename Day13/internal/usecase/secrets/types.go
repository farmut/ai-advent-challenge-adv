// Package secrets detects credentials and personal data in text and masks
// them. It depends only on the standard library, ports and usecase/guard.
package secrets

// Type is a detector type. Its string value is used in configs, HTTP headers
// and audit records.
type Type string

// Detector types.
const (
	APIKeyOpenAI    Type = "api_key_openai"
	APIKeyAnthropic Type = "api_key_anthropic"
	GoogleAPIKey    Type = "google_api_key"
	SlackToken      Type = "slack_token"
	GitHubToken     Type = "github_token"
	AWSAccessKey    Type = "aws_access_key"
	AWSSecretKey    Type = "aws_secret_key"
	JWT             Type = "jwt"
	PrivateKey      Type = "private_key"
	Email           Type = "email"
	Card            Type = "card"
	Phone           Type = "phone"
)

// Placeholders for findings that come from derived views and cannot be
// masked in place.
const (
	PlaceholderEncoded = "[REDACTED_ENCODED_SECRET]"
	PlaceholderJoined  = "[REDACTED_JOINED_SECRET]"
)

var placeholders = map[Type]string{
	APIKeyOpenAI:    "[REDACTED_API_KEY]",
	APIKeyAnthropic: "[REDACTED_API_KEY]",
	GoogleAPIKey:    "[REDACTED_API_KEY]",
	SlackToken:      "[REDACTED_API_KEY]",
	GitHubToken:     "[REDACTED_GITHUB_TOKEN]",
	AWSAccessKey:    "[REDACTED_AWS_KEY]",
	AWSSecretKey:    "[REDACTED_AWS_SECRET]",
	JWT:             "[REDACTED_JWT]",
	PrivateKey:      "[REDACTED_PRIVATE_KEY]",
	Email:           "[REDACTED_EMAIL]",
	Card:            "[REDACTED_CARD]",
	Phone:           "[REDACTED_PHONE]",
}

// Placeholder returns the mask for t, or "[REDACTED]" for an unknown type.
func Placeholder(t Type) string {
	if p, ok := placeholders[t]; ok {
		return p
	}
	return "[REDACTED]"
}

// Types returns all detector types in a stable order.
func Types() []Type {
	return []Type{PrivateKey, APIKeyAnthropic, APIKeyOpenAI, GitHubToken, AWSAccessKey,
		AWSSecretKey, SlackToken, GoogleAPIKey, JWT, Card, Email, Phone}
}

// ValidType reports whether s names a detector type.
func ValidType(s string) bool {
	_, ok := placeholders[Type(s)]
	return ok
}

// isPII reports whether t is personal data rather than a credential. PII is
// not searched in the joined view: concatenation is a code idiom for keys.
func isPII(t Type) bool { return t == Email || t == Card || t == Phone }
