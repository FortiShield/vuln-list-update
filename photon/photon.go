package photon

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/cheggaaa/pb/v3"
	"github.com/spf13/afero"
	"golang.org/x/xerrors"

	"github.com/aquasecurity/vuln-list-update/utils"
)

const (
	advisoryURL    = "https://packages.vmware.com/photon/photon_cve_metadata/"
	versionsFile   = "photon_versions.json"
	advisoryFormat = "cve_data_photon%s.json"

	photonDir = "photon"
	retry     = 5
)

type Config struct {
	VulnListDir string
	URL         string
	AppFs       afero.Fs
	Retry       int
}

func NewConfig() Config {
	return Config{
		VulnListDir: utils.VulnListDir(),
		URL:         advisoryURL,
		AppFs:       afero.NewOsFs(),
		Retry:       retry,
	}
}

func (c Config) getPhotonVersion() ([]string, error) {
	var versions struct {
		Branches []string `json:"branches"`
	}
	res, err := utils.FetchURL(c.URL+versionsFile, "", c.Retry)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch Photon versions: %w", err)
	}
	if err := json.Unmarshal(res, &versions); err != nil {
		return nil, xerrors.Errorf("failed to decode Photon versions: %w", err)
	}

	return versions.Branches, nil
}

func (c Config) fetchAdvisory(version string) ([]byte, error) {
	url := c.URL + fmt.Sprintf(advisoryFormat, version)
	for i := 0; i < c.Retry; i++ {
		res, err := utils.FetchURL(url, "", 1)
		if err == nil {
			return res, nil
		}
		log.Printf("Retrying to fetch Photon advisory for version %s (%d/%d): %v", version, i+1, c.Retry, err)
	}
	return nil, xerrors.Errorf("failed to fetch Photon advisory for version %s after %d retries", version, c.Retry)
}

func (c Config) Update() error {
	log.Printf("Fetching Photon")

	versions, err := c.getPhotonVersion()
	if err != nil {
		return xerrors.Errorf("failed to get Photon versions: %w", err)
	}
	for _, version := range versions {
		if strings.ToLower(version) == "dev" {
			continue
		}
		res, err := c.fetchAdvisory(version)
		if err != nil {
			return xerrors.Errorf("failed to fetch Photon advisory: %w", err)
		}
		var cves []PhotonCVE
		if err := json.Unmarshal(res, &cves); err != nil {
			return xerrors.Errorf("failed to unmarshal Photon advisory: %w", err)
		}
		dir := filepath.Join(photonDir, version)

		bar := pb.StartNew(len(cves))
		for _, def := range cves {
			def.OSVersion = version
			if err = c.saveCVEPerPkg(dir, def.Pkg, def.CveID, def); err != nil {
				return xerrors.Errorf("failed to save CVE-ID per package name: %w", err)
			}
			bar.Increment()
		}
		bar.Finish()
	}

	return nil
}

func (c Config) saveCVEPerPkg(dirName, pkgName, cveID string, data interface{}) error {
	if cveID == "" {
		log.Printf("CVE-ID is empty")
		return nil
	}

	s := strings.Split(cveID, "-")
	if len(s) != 3 {
		log.Printf("invalid CVE-ID: %s", cveID)
		return xerrors.Errorf("invalid CVE-ID format: %s", cveID)
	}

	pkgDir := filepath.Join(c.VulnListDir, dirName, pkgName)
	fileName := fmt.Sprintf("%s.json", cveID)
	if err := utils.WriteJSON(c.AppFs, pkgDir, fileName, data); err != nil {
		return xerrors.Errorf("failed to write file: %w", err)
	}
	return nil
}
