package tracks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pinpoint-research/0.1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return res, nil
}
func ListArchive(ctx context.Context, date, instance string) ([]Asset, error) {
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^(prod|staging)-[0-9]+$`).MatchString(instance) {
		return nil, fmt.Errorf("instance must be prod-N or staging-N")
	}
	tag := "v" + day.Format("2006.01.02") + "-planes-readsb-" + instance
	url := fmt.Sprintf("https://api.github.com/repos/adsblol/globe_history_%d/releases/tags/%s", day.Year(), tag)
	res, err := get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var release struct {
		Assets []Asset `json:"assets"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&release); err != nil {
		return nil, err
	}
	var assets []Asset
	for _, a := range release.Assets {
		if strings.HasPrefix(a.Name, tag+".tar.") {
			assets = append(assets, a)
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	if len(assets) == 0 {
		return nil, fmt.Errorf("no split tar assets in %s", tag)
	}
	return assets, nil
}
func FetchArchive(ctx context.Context, assets []Asset, dir string, maxBytes int64) error {
	return FetchArchiveProgress(ctx, assets, dir, maxBytes, nil)
}

func FetchArchiveProgress(ctx context.Context, assets []Asset, dir string, maxBytes int64, progress func(int64, int64)) error {
	var total int64
	for _, a := range assets {
		if a.Size < 0 || a.Size > maxBytes-total {
			return fmt.Errorf("archive exceeds download limit of %d bytes", maxBytes)
		}
		total += a.Size
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var downloaded int64
	lastProgress := time.Now()
	for _, a := range assets {
		if filepath.Base(a.Name) != a.Name {
			return fmt.Errorf("invalid asset name")
		}
		path := filepath.Join(dir, a.Name)
		// Refuse existing files so a partial previous download cannot masquerade as
		// a complete archive; successful downloads are renamed atomically.
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
		res, err := get(ctx, a.URL)
		if err != nil {
			return err
		}
		f, err := os.CreateTemp(dir, ".download-*")
		if err != nil {
			res.Body.Close()
			return err
		}
		hash := sha256.New()
		writer := io.MultiWriter(f, hash)
		counted := &progressWriter{writer: writer, update: func(n int) {
			downloaded += int64(n)
			if progress != nil && time.Since(lastProgress) > 300*time.Millisecond {
				progress(downloaded, total)
				lastProgress = time.Now()
			}
		}}
		n, copyErr := io.Copy(counted, io.LimitReader(res.Body, a.Size+1))
		res.Body.Close()
		closeErr := f.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		if copyErr == nil && n != a.Size {
			copyErr = fmt.Errorf("size mismatch for %s", a.Name)
		}
		if copyErr == nil && strings.HasPrefix(a.Digest, "sha256:") && "sha256:"+hex.EncodeToString(hash.Sum(nil)) != a.Digest {
			copyErr = fmt.Errorf("SHA256 mismatch for %s", a.Name)
		}
		if copyErr != nil {
			os.Remove(f.Name())
			return copyErr
		}
		if err = os.Rename(f.Name(), path); err != nil {
			os.Remove(f.Name())
			return err
		}
	}
	if progress != nil {
		progress(downloaded, total)
	}
	return nil
}

type progressWriter struct {
	writer io.Writer
	update func(int)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.update(n)
	return n, err
}
