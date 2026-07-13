package postproc

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

func extract(dir string, passwords []string) error {
	deobfuscateRars(dir)
	if found, err := tryExtract(dir, passwords); found {
		return err
	}
	if runJoins(dir) {
		if found, err := tryExtract(dir, passwords); found {
			return err
		}
	}
	return nil
}

func tryExtract(dir string, passwords []string) (bool, error) {
	if first := firstRARVolume(dir); first != "" {
		return true, extractRAR(first, dir, passwords)
	}
	if arc := firstSevenZipVolume(dir); arc != "" {
		return true, extract7z(arc, dir, passwords)
	}
	if z := firstZip(dir); z != "" {
		return true, extract7z(z, dir, passwords)
	}
	return false, nil
}

func extractRAR(first, dir string, passwords []string) error {
	pws := append([]string{""}, passwords...)
	if _, err := exec.LookPath("unrar"); err == nil {
		var lastErr error
		for _, pw := range pws {
			if err := unrarExtract(first, dir, pw); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		slog.Warn("postproc: unrar failed, falling back to rardecode", "err", lastErr)
	}
	var lastErr error
	for _, pw := range pws {
		if err := extractWith(first, dir, pw); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func firstZip(dir string) string {
	if m, _ := filepath.Glob(filepath.Join(dir, "*.zip")); len(m) > 0 {
		sort.Strings(m)
		return m[0]
	}
	return ""
}

func unrarArgs(rarPath, dir, password string) []string {
	args := []string{"e", "-o+", "-y"}
	if password != "" {
		args = append(args, "-p"+password)
	} else {
		args = append(args, "-p-")
	}
	return append(args, rarPath, dir+"/")
}

func unrarExtract(rarPath, dir, password string) error {
	cmd := exec.Command("unrar", unrarArgs(rarPath, dir, password)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func extractWith(rarPath, dir, password string) error {
	var opts []rardecode.Option
	if password != "" {
		opts = append(opts, rardecode.Password(password))
	}
	rc, err := rardecode.OpenReader(rarPath, opts...)
	if err != nil {
		return err
	}
	defer rc.Close()

	for {
		h, err := rc.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.IsDir {
			continue
		}
		out := filepath.Join(dir, filepath.Base(h.Name))
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, rc)
		f.Close()
		if err != nil {
			return err
		}
	}
}

func firstRARVolume(dir string) string {
	for _, pat := range []string{"*.part01.rar", "*.part001.rar", "*.part1.rar"} {
		if m, _ := filepath.Glob(filepath.Join(dir, pat)); len(m) > 0 {
			return m[0]
		}
	}
	rars, _ := filepath.Glob(filepath.Join(dir, "*.rar"))
	for _, r := range rars {
		if !strings.Contains(strings.ToLower(filepath.Base(r)), ".part") {
			return r
		}
	}
	if len(rars) > 0 {
		return rars[0]
	}
	return ""
}

var sevenZipMagic = []byte("7z\xbc\xaf\x27\x1c")

func firstSevenZipVolume(dir string) string {
	for _, pat := range []string{"*.7z.001", "*.7z"} {
		if m, _ := filepath.Glob(filepath.Join(dir, pat)); len(m) > 0 {
			sort.Strings(m)
			return m[0]
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if p := filepath.Join(dir, e.Name()); hasMagic(p, sevenZipMagic) {
			return p
		}
	}
	return ""
}

func sevenZipBinary() string {
	for _, b := range []string{"7zz", "7zzs", "7za", "7z"} {
		if _, err := exec.LookPath(b); err == nil {
			return b
		}
	}
	return ""
}

func extract7z(first, dir string, passwords []string) error {
	bin := sevenZipBinary()
	if bin == "" {
		return fmt.Errorf("7z binary missing")
	}
	var lastErr error
	for _, pw := range append([]string{""}, passwords...) {
		pflag := "-p"
		if pw != "" {
			pflag = "-p" + pw
		}
		cmd := exec.Command(bin, "e", "-y", "-aoa", "-ssc", pflag, "-o"+dir, first)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return lastErr
}
