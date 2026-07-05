package postproc

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/nwaples/rardecode/v2"
	"github.com/torrin-app/torrin/shared/video"
)

type File struct {
	Name string
	Path string
	Size int64
}

func Process(dir string, passwords []string, release string) ([]File, error) {
	deobfuscateByPar2(dir)
	repair(dir)
	if err := extract(dir, passwords); err != nil {
		return nil, err
	}
	logDir(dir)
	return nameVideos(collectVideos(dir), release), nil
}

func nameVideos(files []File, release string) []File {
	release = filepath.Base(strings.TrimSpace(release))
	single := len(files) == 1
	for i, f := range files {
		ext := videoExt(f.Path, f.Name)
		var newName string
		switch {
		case single && release != "" && release != ".":
			newName = release + ext
		case !video.IsVideo(f.Name):
			newName = strings.TrimSuffix(f.Name, filepath.Ext(f.Name)) + ext
		default:
			continue
		}
		if newName == "" || newName == f.Name {
			continue
		}
		newPath := filepath.Join(filepath.Dir(f.Path), newName)
		if err := os.Rename(f.Path, newPath); err != nil {
			slog.Warn("postproc: rename failed", "from", f.Name, "to", newName, "err", err)
			continue
		}
		slog.Info("postproc: named video", "from", f.Name, "to", newName)
		files[i].Name, files[i].Path = newName, newPath
	}
	return files
}

func videoExt(path, name string) string {
	if video.IsVideo(name) {
		return strings.ToLower(filepath.Ext(name))
	}
	if mtype, err := mimetype.DetectFile(path); err == nil {
		if e := mtype.Extension(); e != "" {
			return e
		}
	}
	return filepath.Ext(name)
}

func logDir(dir string) {
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		size := int64(0)
		if info, e := d.Info(); e == nil {
			size = info.Size()
		}
		slog.Info("postproc: scratch file", "name", d.Name(), "size", size, "is_video", video.IsVideo(d.Name()))
		return nil
	})
}

func repair(dir string) {
	par2 := findPar2(dir)
	if par2 == "" {
		slog.Info("postproc: no par2 set found, skipping repair", "dir", dir)
		return
	}
	if _, err := exec.LookPath("par2"); err != nil {
		slog.Warn("postproc: par2 binary missing, cannot repair/deobfuscate")
		return
	}
	cmd := exec.Command("par2", "repair", filepath.Base(par2))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	tail := strings.TrimSpace(string(out))
	if len(tail) > 400 {
		tail = tail[len(tail)-400:]
	}
	slog.Info("postproc: par2 repair", "file", filepath.Base(par2), "err", err, "out", tail)
}

var par2Magic = []byte("PAR2\x00PKT")

func findPar2(dir string) string {
	if m, _ := filepath.Glob(filepath.Join(dir, "*.par2")); len(m) > 0 {
		sort.Strings(m)
		return m[0]
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if p := filepath.Join(dir, e.Name()); hasMagic(p, par2Magic) {
			return p
		}
	}
	return ""
}

func hasMagic(path string, magic []byte) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(magic))
	if _, err := io.ReadFull(f, buf); err != nil {
		return false
	}
	return bytes.Equal(buf, magic)
}

func extract(dir string, passwords []string) error {
	deobfuscateRars(dir)
	first := firstRARVolume(dir)
	if first == "" {
		return nil
	}
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

func collectVideos(dir string) []File {
	var out []File
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isVideoFile(path, d.Name()) {
			return nil
		}
		size := int64(0)
		if info, err := d.Info(); err == nil {
			size = info.Size()
		}
		out = append(out, File{Name: d.Name(), Path: path, Size: size})
		return nil
	})
	return out
}

func isVideoFile(path, name string) bool {
	if video.IsVideo(name) {
		return true
	}
	mtype, err := mimetype.DetectFile(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(mtype.String(), "video/")
}

func PasswordCandidates(metaPassword string, names ...string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p = strings.TrimSpace(p); p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(metaPassword)
	for _, n := range names {
		add(passwordFromName(n))
	}
	return out
}

func passwordFromName(name string) string {
	if i := strings.Index(name, "{{"); i >= 0 {
		if j := strings.Index(name[i+2:], "}}"); j >= 0 {
			if pw := strings.TrimSpace(name[i+2 : i+2+j]); pw != "" {
				return pw
			}
		}
	}
	if i := strings.Index(strings.ToLower(name), "password="); i >= 0 {
		v := name[i+len("password="):]
		if end := strings.IndexAny(v, " \t\r\n&}"); end >= 0 {
			v = v[:end]
		}
		return strings.TrimSpace(v)
	}
	return ""
}
