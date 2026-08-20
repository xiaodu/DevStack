package registry

type Category string

const (
	CatWebServer Category = "webserver"
	CatDatabase  Category = "database"
	CatRuntime   Category = "runtime"
	CatService   Category = "service"
)

type PackageType string

const (
	PkgNginx    PackageType = "nginx"
	PkgApache   PackageType = "apache"
	PkgMySQL    PackageType = "mysql"
	PkgPostgreSQL PackageType = "postgresql"
	PkgSQLite   PackageType = "sqlite"
	PkgMongoDB  PackageType = "mongodb"
	PkgRedis    PackageType = "redis"
	PkgMemcached PackageType = "memcached"
	PkgPHP      PackageType = "php"
	PkgNode     PackageType = "node"
	PkgPython   PackageType = "python"
	PkgComposer PackageType = "composer"
)

type VersionInfo struct {
	Version  string `json:"version"`
	URL      string `json:"url"`
	Mirror   string `json:"mirror,omitempty"`
	FileName string `json:"fileName"`
	Checksum string `json:"checksum,omitempty"`
}

type PackageDef struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Type           PackageType   `json:"type"`
	Category       Category      `json:"category"`
	Versions       []VersionInfo `json:"versions"`
	DefaultVersion string        `json:"defaultVersion"`
	DefaultPort    int           `json:"defaultPort"`
}

type Manifest struct {
	Packages []PackageDef `json:"packages"`
}

func (m *Manifest) Get(id string) *PackageDef {
	for _, p := range m.Packages {
		if p.ID == id {
			return &p
		}
	}
	return nil
}

func (m *Manifest) List(cat Category) []PackageDef {
	if cat == "" {
		return m.Packages
	}
	var out []PackageDef
	for _, p := range m.Packages {
		if p.Category == cat {
			out = append(out, p)
		}
	}
	return out
}
