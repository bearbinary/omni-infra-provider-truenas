package client

// SecretString wraps a sensitive string to prevent accidental leakage
// in logs, fmt.Sprint, error messages, and JSON serialization.
// The actual value is only accessible via the Reveal() method.
type SecretString struct {
	value string
}

// NewSecretString creates a SecretString from a plaintext value.
func NewSecretString(s string) SecretString {
	return SecretString{value: s}
}

// Reveal returns the actual secret value. Use only when sending
// the value to its intended destination (auth headers, API calls).
func (s SecretString) Reveal() string {
	return s.value
}

// String returns a redacted placeholder. This prevents the secret
// from appearing in fmt.Sprint, log messages, or error wrapping.
func (s SecretString) String() string {
	return "[REDACTED]"
}

// GoString returns a redacted placeholder for %#v formatting.
func (s SecretString) GoString() string {
	return "SecretString{[REDACTED]}"
}

// MarshalJSON returns a redacted JSON string to prevent serialization leaks.
func (s SecretString) MarshalJSON() ([]byte, error) {
	return []byte(`"[REDACTED]"`), nil
}

// IsEmpty returns true if the secret has no value.
func (s SecretString) IsEmpty() bool {
	return s.value == ""
}
