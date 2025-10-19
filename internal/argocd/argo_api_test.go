package argocd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type stubBody struct {
	data     []byte
	readErr  error
	closeErr error
	offset   int
}

func (b *stubBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}

	if b.offset >= len(b.data) {
		return 0, io.EOF
	}

	n := copy(p, b.data[b.offset:])
	b.offset += n

	if b.offset >= len(b.data) {
		return n, io.EOF
	}

	return n, nil
}

func (b *stubBody) Close() error {
	return b.closeErr
}

func TestArgoApiInit(t *testing.T) {
	argoURL, err := url.Parse("https://example.com")
	require.NoError(t, err)

	cfg := &config.ServerConfig{
		ArgoUrl:        *argoURL,
		ArgoToken:      "super-secret",
		ArgoApiTimeout: 42,
		SkipTlsVerify:  true,
		ArgoRefreshApp: true,
	}

	api := &ArgoApi{}
	require.NoError(t, api.Init(cfg))

	assert.Equal(t, cfg.ArgoUrl, api.baseUrl)
	require.NotNil(t, api.client)
	assert.Equal(t, time.Duration(cfg.ArgoApiTimeout)*time.Second, api.client.Timeout)
	require.NotNil(t, api.client.Jar)
	cookies := api.client.Jar.Cookies(argoURL)
	require.Len(t, cookies, 1)
	assert.Equal(t, "argocd.token", cookies[0].Name)
	assert.Equal(t, cfg.ArgoToken, cookies[0].Value)

	transport, ok := api.client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.TLSClientConfig)
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
	assert.True(t, api.refreshApp)

	t.Run("returnsErrorWhenCookieJarFails", func(t *testing.T) {
		api := &ArgoApi{}
		original := newCookieJar
		called := false
		newCookieJar = func(o *cookiejar.Options) (*cookiejar.Jar, error) {
			called = true
			return nil, errors.New("jar error")
		}
		t.Cleanup(func() { newCookieJar = original })

		err := api.Init(cfg)
		assert.EqualError(t, err, "jar error")
		assert.True(t, called)
	})
}

func TestArgoApiGetUserInfoSuccess(t *testing.T) {
	expected := models.Userinfo{Username: "tester", LoggedIn: true}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/session/userinfo", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(expected))
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	api := &ArgoApi{baseUrl: *parsedURL, client: server.Client()}
	userInfo, err := api.GetUserInfo()
	require.NoError(t, err)
	assert.Equal(t, &expected, userInfo)
}

func TestArgoApiGetUserInfoErrors(t *testing.T) {
	baseURL, err := url.Parse("https://example.com")
	require.NoError(t, err)

	successJSON := []byte(`{"loggedIn":true,"username":"ok"}`)

	testCases := []struct {
		name     string
		api      *ArgoApi
		setup    func(t *testing.T)
		wantErr  bool
		wantUser *models.Userinfo
	}{
		{
			name: "requestCreationError",
			api:  &ArgoApi{baseUrl: *baseURL, client: &http.Client{}},
			setup: func(t *testing.T) {
				original := httpNewRequest
				httpNewRequest = func(string, string, io.Reader) (*http.Request, error) {
					return nil, errors.New("build request error")
				}
				t.Cleanup(func() {
					httpNewRequest = original
				})
			},
			wantErr: true,
		},
		{
			name: "clientDoError",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return nil, errors.New("network error")
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "readBodyError",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       &stubBody{data: successJSON, readErr: errors.New("read failure")},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "unmarshalError",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       &stubBody{data: []byte(`{invalid-json}`)},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "closeErrorLoggedButIgnored",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       &stubBody{data: successJSON, closeErr: errors.New("close failure")},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr:  false,
			wantUser: &models.Userinfo{LoggedIn: true, Username: "ok"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			userInfo, err := tc.api.GetUserInfo()
			if tc.wantErr {
				assert.Error(t, err)
				assert.Nil(t, userInfo)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.wantUser, userInfo)
			}
		})
	}
}

func TestArgoApiGetApplicationSuccess(t *testing.T) {
	app := models.Application{}
	app.Status.Health.Status = "Healthy"
	app.Status.Sync.Status = "Synced"
	app.Status.Summary.Images = []string{"example.com/image:v1.0.0"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/applications/demo", r.URL.Path)
		assert.Empty(t, r.URL.Query().Get("refresh"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(app))
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	api := &ArgoApi{baseUrl: *parsedURL, client: server.Client()}
	result, err := api.GetApplication("demo")
	require.NoError(t, err)
	assert.Equal(t, app.Status.Health.Status, result.Status.Health.Status)
	assert.Equal(t, app.Status.Sync.Status, result.Status.Sync.Status)
	assert.Equal(t, app.Status.Summary.Images, result.Status.Summary.Images)
}

func TestArgoApiGetApplicationAddsRefreshQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "refresh=normal", r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(models.Application{}))
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	api := &ArgoApi{baseUrl: *parsedURL, client: server.Client(), refreshApp: true}
	_, err = api.GetApplication("demo")
	assert.NoError(t, err)
}

func TestArgoApiGetApplicationErrors(t *testing.T) {
	baseURL, err := url.Parse("https://example.com")
	require.NoError(t, err)

	validErrorResponse, err := json.Marshal(models.ArgoApiErrorResponse{Message: "boom"})
	require.NoError(t, err)
	errorWithoutMessage, err := json.Marshal(models.ArgoApiErrorResponse{Message: ""})
	require.NoError(t, err)

	testCases := []struct {
		name    string
		api     *ArgoApi
		setup   func(t *testing.T)
		wantErr bool
	}{
		{
			name: "requestCreationError",
			api:  &ArgoApi{baseUrl: *baseURL, client: &http.Client{}},
			setup: func(t *testing.T) {
				original := httpNewRequest
				httpNewRequest = func(string, string, io.Reader) (*http.Request, error) {
					return nil, errors.New("build request error")
				}
				t.Cleanup(func() {
					httpNewRequest = original
				})
			},
			wantErr: true,
		},
		{
			name: "clientDoError",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return nil, errors.New("connection refused")
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "readBodyError",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       &stubBody{data: []byte(`{}`), readErr: errors.New("read failure")},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "jsonUnmarshalErrorOnSuccess",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       &stubBody{data: []byte(`{invalid}`)},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "non200WithValidMessage",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusBadRequest,
							Body:       &stubBody{data: validErrorResponse},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "non200WithInvalidJSON",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusBadRequest,
							Body:       &stubBody{data: []byte(`{invalid}`)},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "non200WithEmptyMessage",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: http.StatusBadRequest,
							Body:       &stubBody{data: errorWithoutMessage},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "closeErrorIgnored",
			api: &ArgoApi{
				baseUrl: *baseURL,
				client: &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						app := models.Application{}
						app.Status.Health.Status = "Healthy"
						app.Status.Sync.Status = "Synced"
						data, _ := json.Marshal(app)
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       &stubBody{data: data, closeErr: errors.New("close failure")},
							Header:     make(http.Header),
						}, nil
					}),
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			app, err := tc.api.GetApplication("demo")
			if tc.wantErr {
				assert.Error(t, err)
				assert.Nil(t, app)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, app)
			}
		})
	}
}
