package hasNoWg

// Plain struct — no goroutine ownership, no lifecycle test needed.
type Owner struct {
	name string
}
