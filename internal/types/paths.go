package types

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

type Paths struct {
	Download string
	Install  string
	Db       string
}

func (p *Paths) GetDownloadPath() string {
	return p.Download
}

func (p *Paths) GetInstallPath() string {
	return p.Install
}

func (p *Paths) GetDbPath() string {
	return p.Db
}

func NewPaths() *Paths {
	paths := &Paths{}

	paths.Download = filepath.Join(xdg.UserDirs.Download, "WiNFerior")
	paths.Install = xdg.FontDirs[0]
	paths.Db = filepath.Join(xdg.DataHome, "WiNFerior")

	os.MkdirAll(paths.Download, 0755)
	os.MkdirAll(paths.Install, 0755)
	os.MkdirAll(paths.Db, 0755)

	return paths
}