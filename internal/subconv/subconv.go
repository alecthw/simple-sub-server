package subconv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/alecthw/sub-server/internal/filestore"
	"github.com/alecthw/sub-server/internal/subscription"
	"github.com/go-resty/resty/v2"
	"gopkg.in/ini.v1"
)

// Context carries dependencies for ini/subconverter handling.
type Context struct {
	RequestContext     context.Context
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

var ErrUpstream = errors.New("subconverter unavailable")

// Handle processes an ini file as either a redirect or a subconverter profile.
func Handle(ctx Context, content []byte) (Response, error) {
	cfgs, err := ini.Load(content)
	if err != nil {
		return Response{}, err
	}

	if cfgs.HasSection("Redirect") {
		return redirectResponse(ctx, cfgs.Section("Redirect"))
	}

	body, err := getSubconv(ctx, cfgs)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Status:      http.StatusOK,
		ContentType: "text/plain; charset=UTF-8",
		Body:        body,
	}, nil
}

func redirectResponse(ctx Context, section *ini.Section) (Response, error) {
	nextFile := strings.TrimSpace(section.Key("file").String())
	if nextFile == "" || !filestore.SafeName(nextFile) {
		return Response{}, os.ErrNotExist
	}

	return Response{
		Status:   http.StatusFound,
		Location: ctx.RedirectURLForFile(nextFile),
	}, nil
}

func getSubconv(ctx Context, cfgs *ini.File) ([]byte, error) {
	if !cfgs.Section("Profile").HasKey("url") {
		_, _ = cfgs.Section("Profile").NewKey("url", subscription.JoinURLs(ctx.Entries))
	}

	request := ctx.Client.R()
	if ctx.RequestContext != nil {
		request.SetContext(ctx.RequestContext)
	}
	resp, err := request.
		SetQueryParams(cfgs.Section("Profile").KeysHash()).
		Get(ctx.SubconverterURL + "/sub")
	if err != nil {
		return nil, ErrUpstream
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode())
	}
	target := cfgs.Section("Profile").Key("target").String()
	if ctx.ManagedConfigURL != "" && (target == "surge" || target == "surfboard") {
		header := "#!MANAGED-CONFIG " + ctx.ManagedConfigURL + " interval=43200 strict=true\n"
		return append([]byte(header), resp.Body()...), nil
	}

	return resp.Body(), nil
}
