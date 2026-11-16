package release

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type developmentTransport struct{ payload string }

func (d developmentTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(d.payload))}, nil
}

// DevelopmentFixture preserves full regression coverage only with the existing
// explicit synthetic identity fixture; production issuer configuration is refused.
func DevelopmentFixture(environment, issuer, enabled string) (*Service, error) {
	if environment != "dev" || issuer != "http://auth.tesserix.localhost:20080" || enabled != "true" {
		return nil, errors.New("release fixture requires the explicit local synthetic identity environment")
	}
	features := map[string]any{}
	for _, definition := range definitions {
		features[flagPrefix+definition.Key] = map[string]bool{"defaultValue": true}
	}
	body, err := json.Marshal(map[string]any{"features": features})
	if err != nil {
		return nil, err
	}
	service, err := New(Config{URL: "https://synthetic-release.invalid", ClientKey: "synthetic", Client: &http.Client{Transport: developmentTransport{payload: string(body)}}})
	if err != nil {
		return nil, err
	}
	service.developmentFixture = true
	return service, nil
}
