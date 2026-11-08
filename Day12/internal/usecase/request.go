package usecase

// Request is one consultation: the user's question and an optional attached
// reference document. Both are untrusted data.
type Request struct {
	Question string
	Context  string
}
