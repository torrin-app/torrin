package rclonerc

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

func (c *Client) UploadFile(ctx context.Context, fs, dir, name string, body io.Reader) error {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", name)
		if err == nil {
			_, err = io.Copy(part, body)
		}
		if cerr := mw.Close(); err == nil {
			err = cerr
		}
		pw.CloseWithError(err)
	}()

	u := fmt.Sprintf("%s/operations/uploadfile?fs=%s&remote=%s", c.base, url.QueryEscape(fs), url.QueryEscape(dir))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &Error{Method: "operations/uploadfile", Status: resp.StatusCode, Msg: string(msg)}
	}
	return nil
}
