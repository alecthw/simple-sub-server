package subconv

import (
	"bytes"
	"net/http"
	"os"
	"strings"

	"github.com/alecthw/sub-server/handler/subscription"
	"github.com/go-resty/resty/v2"
	"gopkg.in/ini.v1"
)

// Context carries dependencies for ini/subconverter handling.
type Context struct {
	UID                string
	File               string
	ManagedConfigURL   string
	SubconverterURL    string
	Client             *resty.Client
	Entries            []subscription.Entry
	RedirectURLForFile func(file string) string
}

// Response represents the HTTP response generated from an ini file.
type Response struct {
	Status      int
	ContentType string
	Body        []byte
	Location    string
}

// Handle processes an ini file as either a redirect or a subconverter profile.
func Handle(ctx Context, content []byte) (Response, error) {
	cfgs, err := ini.Load(content)
	if err != nil {
		return Response{}, err
	}

	if cfgs.HasSection("Redirect") {
		return redirectResponse(ctx, cfgs.Section("Redirect"))
	}

	return Response{
		Status:      http.StatusOK,
		ContentType: "text/plain; charset=UTF-8",
		Body:        getSubconv(ctx, cfgs),
	}, nil
}

func redirectResponse(ctx Context, section *ini.Section) (Response, error) {
	nextFile := strings.TrimSpace(section.Key("file").String())
	if nextFile == "" || !isPathSecure(nextFile) {
		return Response{}, os.ErrNotExist
	}

	return Response{
		Status:   http.StatusFound,
		Location: ctx.RedirectURLForFile(nextFile),
	}, nil
}

func getSubconv(ctx Context, cfgs *ini.File) []byte {
	if !cfgs.Section("Profile").HasKey("url") {
		_, _ = cfgs.Section("Profile").NewKey("url", subscription.JoinURLs(ctx.Entries))
	}

	resp, err := ctx.Client.R().
		SetQueryParams(cfgs.Section("Profile").KeysHash()).
		Get(ctx.SubconverterURL + "/sub")
	if err != nil {
		return []byte("")
	}

	target := cfgs.Section("Profile").Key("target").String()
	if ctx.ManagedConfigURL != "" && (target == "surge" || target == "surfboard") {
		var buffer bytes.Buffer
		buffer.Write([]byte("#!MANAGED-CONFIG " + ctx.ManagedConfigURL + " interval=43200 strict=true\n"))
		buffer.Write(resp.Body())
		return buffer.Bytes()
	}

	return resp.Body()
}

func isPathSecure(filePath string) bool {
	return !strings.Contains(filePath, "..") && !strings.Contains(filePath, "/") && !strings.Contains(filePath, "\\")
}
