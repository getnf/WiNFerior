package handlers

import (
	"archive/tar"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/briandowns/spinner"
	"github.com/getnf/winferior/internal/db"
	"github.com/getnf/winferior/internal/types"
	"github.com/getnf/winferior/internal/utils"
	"github.com/lithammer/fuzzysearch/fuzzy"

	"github.com/ulikunitz/xz"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func SetupDB(database *sql.DB, remoteData types.NerdFonts) {
	db.CreateVersionTable(database)
	db.CreateFontsTable(database)
	db.CreateInstalledFontsTable(database)

	if db.TableIsEmpty(database, "version") || IsUpdateAvilable(remoteData.GetVersion(), db.GetVersion(database)) {
		db.InsertIntoVersion(database, remoteData.GetVersion())
		fmt.Println("Updated fonts version")
	}

	if db.TableIsEmpty(database, "fonts") || IsUpdateAvilable(remoteData.GetVersion(), db.GetVersion(database)) {
		db.DeleteFontsTable(database)
		db.CreateFontsTable(database)
		db.InsertIntoFonts(database, remoteData.GetFonts())
		fmt.Println("Updating local fonts db")
	}
}

func GetData() (types.NerdFonts, error) {
	url := "https://api.github.com/repos/ryanoasis/nerd-fonts/releases/latest"
	resp, err := http.Get(url)
	if err != nil {
		return types.NerdFonts{}, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NerdFonts{}, err
	}

	var data types.NerdFonts
	err = json.Unmarshal(body, &data)
	if err != nil {
		log.Fatalln(err)
	}
	return data, nil
}

func downloadFont(fontURL string, path string, name string) (string, error) {
	fullPath := path + "/" + name + ".tar.xz"
	resp, err := http.Get(fontURL)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", err
	}

	// Make sure the path exists
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(path, os.ModePerm)
		if err != nil {
			return "", err
		}
	}

	// Create the file
	out, err := os.Create(fullPath)
	if err != nil {
		return "", err
	}

	defer out.Close()
	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", err
	}

	return fullPath, nil
}

// extractTar extracts files from a tar archive provided in the reader
func extractFont(archivePath string, extractPath string, name string) ([]string, error) {
	var listOfInstalledFonts []string

	// Decompress the xz stream
	fontArchive, err := os.Open(archivePath)
	if err != nil {
		return []string{""}, err
	}
	xzReader, err := xz.NewReader(fontArchive)
	if err != nil {
		return []string{""}, err
	}

	defer fontArchive.Close()

	// Create a tar reader from the decompressed stream
	tarReader := tar.NewReader(xzReader)

	// Iterate over each file in the tar archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			// End of tar archive
			break
		}
		if err != nil {
			return []string{""}, err
		}

		// Extract the file name from the header
		fullPath := filepath.Join(extractPath, name, header.Name)
		extractPath := filepath.Join(extractPath, name)

		// Create directories if they don't exist, if the tar contains directories
		if header.Typeflag == tar.TypeDir {
			err := os.MkdirAll(fullPath, 0755)
			if err != nil {
				return []string{""}, err
			}
			continue
		}

		if _, err := os.Stat(extractPath); errors.Is(err, os.ErrNotExist) {
			err := os.Mkdir(extractPath, os.ModePerm)
			if err != nil {
				return []string{""}, err
			}
		}

		// Create file with same permissions as in the tar file
		file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
		if err != nil {
			return []string{""}, err
		}
		defer file.Close()

		// Write file content to disk
		_, err = io.Copy(file, tarReader)
		if err != nil {
			return []string{""}, err
		}

		listOfInstalledFonts = append(listOfInstalledFonts, header.Name)
	}

	return listOfInstalledFonts, nil
}

func InstallFont(font types.Font, downloadPath string, extractPath string, keepTar bool) error {
	downloadedTar, err := downloadFont(font.BrowserDownloadUrl, downloadPath, font.Name)
	if err != nil {
		return fmt.Errorf("error downloading the tar file: %v", err)
	}
	extractedTar, err := extractFont(downloadedTar, extractPath, font.Name)
	if err != nil {
		return fmt.Errorf("error extracting the tar file: %v", err)
	}
	for _, fileName := range extractedTar {
		err = removeFromRegistry(fileName)
		if err != nil {
			log.Fatalln(err)
		}
		err = writeToRegistry(extractPath, font.Name, fileName)
		if err != nil {
			log.Fatalln(err)
		}
	}
	if !keepTar {
		deleteTar(downloadedTar)
	}

	return nil
}

func UninstallFont(path string, name string) error {
	fontPath := filepath.Join(path, name)
	fontFiles, err := os.ReadDir(fontPath)
	if err != nil {
		log.Fatalln(err)
	}

	var fileNames []string

	if _, err := os.Stat(fontPath); os.IsNotExist(err) {
		return fmt.Errorf("font %v is not installed", name)
	} else {
		for _, file := range fontFiles {
			fileNames = append(fileNames, file.Name())
		}

		err = os.RemoveAll(fontPath)
		if err != nil {
			return err
		}
		for _, file := range fileNames {
			removeFromRegistry(file)
		}
	}
	return nil
}

func deleteTar(tarPath string) error {
	if _, err := os.Stat(tarPath); os.IsNotExist(err) {
		return fmt.Errorf("tar file does not exist")
	} else {
		err = os.Remove(tarPath)
		if err != nil {
			return err
		}
	}
	return nil
}

func IsUpdateAvilable(remote string, local string) bool {
	remoteVersion, err := utils.StringToInt(remote)
	if err != nil {
		log.Fatalln(err)
	}

	localVersion, err := utils.StringToInt(local)
	if err != nil {
		log.Fatalln(err)
	}
	if remoteVersion > localVersion {
		return true
	} else {
		return false
	}
}

func IsFontUpdatAvilable(database *sql.DB, data types.NerdFonts) bool {
	updateCount := 0
	installedFonts := db.GetInstalledFonts(database)
	for _, font := range installedFonts {
		if IsUpdateAvilable(data.GetVersion(), font.InstalledVersion) {
			updateCount++
		}
	}

	return updateCount > 0
}

func HandleUpdate(database *sql.DB, data types.NerdFonts, downloadPath string, extractPath string) error {
	if IsFontUpdatAvilable(database, data) {
		installedFonts := db.GetInstalledFonts(database)
		for _, font := range installedFonts {
			f := data.GetFont(font.Name)
			err := InstallFont(f, downloadPath, extractPath, false)
			if err != nil {
				return err
			}
			db.UpdateInstalledFont(database, font.Name, data.GetVersion())
		}
	} else {
		fmt.Println("No updates are available")
	}

	return nil
}

func FuzzySearchFonts(font string, fonts []string) ([]string, error) {
	matches := fuzzy.RankFindFold(font, fonts)
	var match []string
	sort.Sort(matches)

	if len(matches) > 0 {
		var topMatches fuzzy.Ranks
		if len(matches) > 3 {
			topMatches = matches[0:3]
		} else {
			size := len(matches)
			topMatches = matches[0:size]
		}
		for _, font := range topMatches {
			match = append(match, font.Target)
		}
	} else {
		return []string{""}, fmt.Errorf("no match found")
	}
	return match, nil
}

func writeToRegistry(path string, fontName string, fileName string) error {
	fullPath := filepath.Join(path, fontName, fileName)
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`,
		registry.WRITE)
	if err != nil {
		os.Remove(fullPath)
		return fmt.Errorf("error opening registry key: %w", err)
	}
	defer k.Close()

	valueName := fmt.Sprintf("%s (TrueType)", fileName)
	err = k.SetStringValue(valueName, fullPath)
	if err != nil {
		os.Remove(fullPath)
		return fmt.Errorf("error writing to registry: %w", err)
	}

	return nil
}

func removeFromRegistry(name string) error {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`,
		registry.WRITE)
	if err != nil {
		return fmt.Errorf("error opening registry key: %w", err)
	}
	defer k.Close()

	valueName := fmt.Sprintf("%s (TrueType)", name)

	// Check if the value exists before attempting to remove it
	exists, err := valueExistsInRegistry(k, valueName)
	if err != nil {
		return fmt.Errorf("error checking if value exists: %w", err)
	}
	if !exists {
		return nil
	}

	err = k.DeleteValue(valueName)
	if err != nil {
		return fmt.Errorf("error deleting registry value: %w", err)
	}

	return nil
}

func valueExistsInRegistry(key registry.Key, name string) (bool, error) {
	k, err := registry.OpenKey(key, "", registry.QUERY_VALUE)
	if err != nil {
		return false, fmt.Errorf("error opening registry key: %w", err)
	}
	defer k.Close()
	_, _, err = k.GetStringValue(name)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func IsAdmin() bool {
	if windows.GetCurrentProcessToken().IsElevated() {
		return true
	}

	return false
}

func FontsWithVersion(database *sql.DB, fonts []types.Font, version string) []types.Font {
	var results []types.Font
	for _, font := range fonts {
		if db.IsFontInstalled(database, font.Name) {
			installedFont := db.GetInstalledFont(database, font)
			font.AddInstalledVersion(installedFont.InstalledVersion)
		} else {
			font.AddInstalledVersion("-")
		}
		font.AddAvailableVersion(version)
		results = append(results, font)
	}
	return results
}

func ListFonts(fonts []types.Font, onlyInstalled bool) {
	isInstalledFont := func(x types.Font) bool { return x.InstalledVersion != "-" }
	if onlyInstalled {
		fonts = utils.Filter(fonts, isInstalledFont)
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 8, 4, '\t', tabwriter.AlignRight)

	fmt.Fprintln(writer, "Name:\tAvailable Version:\tInstalled Version:")

	if len(fonts) == 0 && onlyInstalled {
		fmt.Println("No fonts have been installed yet")
		return
	}
	for _, font := range fonts {
		fmt.Fprintln(writer, font.Name, "\t", font.AvailableVersion, "\t", font.InstalledVersion)
	}
	writer.Flush()
}

func HandleInstall(args types.Args, database *sql.DB, data types.NerdFonts, downloadPath string, extractPath string) error {
	var installedFonts []string
	var fontsToInstall []string
	for _, font := range args.Install.Fonts {
		if db.FontExists(database, font) {
			fontsToInstall = append(fontsToInstall, font)
		} else {
			fmt.Printf("%v is not a nerd font\n", font)
			fuzzySearchedFont, err := FuzzySearchFonts(font, data.GetFontsNames())
			if err != nil {
				return fmt.Errorf("did you mean: %v: ", err)
			}
			// fmt.Printf("did you mean: %v\n", fuzzySearchedFont)
			return fmt.Errorf("did you mean: %v: ", fuzzySearchedFont)
		}
	}
	if len(fontsToInstall) > 0 {
		for _, font := range fontsToInstall {
			f := data.GetFont(font)
			err := InstallFont(f, downloadPath, extractPath, args.KeepTars)
			if err != nil {
				return err
			}
			db.InsertIntoInstalledFonts(database, f, data.GetVersion())
			installedFonts = append(installedFonts, font)
		}
	}
	if len(installedFonts) > 0 {
		fmt.Printf("Installed font(s): %v\n", strings.Join(installedFonts, ", "))
	}

	return nil
}

func HandleUninstall(args types.Args, database *sql.DB, data types.NerdFonts, extractPath string) error {
	var fontsToUninstall []string
	for _, font := range args.Uninstall.Fonts {
		if db.IsFontInstalled(database, font) {
			fontsToUninstall = append(fontsToUninstall, font)
		} else {
			fmt.Printf("%v is either not installed or is not a nerd font\n", font)
			fuzzySearchedFont, err := FuzzySearchFonts(font, data.GetFontsNames())
			if err != nil {
				return fmt.Errorf("did you mean: %v: ", err)
			}
			return fmt.Errorf("did you mean: %v: ", fuzzySearchedFont)
		}
	}
	if len(fontsToUninstall) > 0 {
		s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
		s.Suffix = " Uninstalling fonts"
		s.Color("red")
		s.Start()
		for _, font := range fontsToUninstall {
			err := UninstallFont(extractPath, font)
			if err != nil {
				s.Stop()
				return err
			}
			db.DeleteInstalledFont(database, font)
		}
		s.FinalMSG = "uninstalled font(s): " + strings.Join(fontsToUninstall, ", ") + "\n"
		s.Stop()
	}

	return nil
}
