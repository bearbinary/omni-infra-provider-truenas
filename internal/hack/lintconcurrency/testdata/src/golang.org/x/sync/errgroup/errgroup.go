// Minimal errgroup stub for analysistest. The analyzer inspects the type
// syntax only — it never resolves the Group methods — so a bare struct
// declaration is sufficient.
package errgroup

type Group struct{}

func (g *Group) Wait() error { return nil }
func (g *Group) Go(func() error) {}
