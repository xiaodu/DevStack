package project

type RuntimePortGetter interface {
	GetPHPVersionPort(version string) int
}
