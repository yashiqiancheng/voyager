// Copyright 2021 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package debugapi_test

import (
	"net/http"
	"testing"
)

func TestCORSHeaders(t *testing.T) {
	for _, tc := range []struct {
		name           string
		origin         string
		allowedOrigins []string
		wantCORS       bool
	}{
		{
			name: "none",
		},
		{
			name:           "no origin",
			allowedOrigins: []string{"https://gateway.ethinfinity.org"},
			wantCORS:       false,
		},
		{
			name:           "single explicit",
			origin:         "https://gateway.ethinfinity.org",
			allowedOrigins: []string{"https://gateway.ethinfinity.org"},
			wantCORS:       true,
		},
		{
			name:           "single explicit blocked",
			origin:         "http://a-hacker.me",
			allowedOrigins: []string{"https://gateway.ethinfinity.org"},
			wantCORS:       false,
		},
		{
			name:           "multiple explicit",
			origin:         "https://staging.gateway.ethinfinity.org",
			allowedOrigins: []string{"https://gateway.ethinfinity.org", "https://staging.gateway.ethinfinity.org"},
			wantCORS:       true,
		},
		{
			name:           "multiple explicit blocked",
			origin:         "http://a-hacker.me",
			allowedOrigins: []string{"https://gateway.ethinfinity.org", "https://staging.gateway.ethinfinity.org"},
			wantCORS:       false,
		},
		{
			name:           "wildcard",
			origin:         "http://localhost:1234",
			allowedOrigins: []string{"*"},
			wantCORS:       true,
		},
		{
			name:           "wildcard",
			origin:         "https://gateway.ethinfinity.org",
			allowedOrigins: []string{"*"},
			wantCORS:       true,
		},
		{
			name:           "with origin only",
			origin:         "https://gateway.ethinfinity.org",
			allowedOrigins: nil,
			wantCORS:       false,
		},
		{
			name:           "with origin only not nil",
			origin:         "https://gateway.ethinfinity.org",
			allowedOrigins: []string{},
			wantCORS:       false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testServer := newTestServer(t, testServerOptions{
				CORSAllowedOrigins: tc.allowedOrigins,
			})

			req, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			r, err := testServer.Client.Do(req)
			if err != nil {
				t.Fatal(err)
			}

			got := r.Header.Get("Access-Control-Allow-Origin")

			if tc.wantCORS {
				if got != tc.origin {
					t.Errorf("got Access-Control-Allow-Origin %q, want %q", got, tc.origin)
				}
			} else {
				if got != "" {
					t.Errorf("got Access-Control-Allow-Origin %q, want none", got)
				}
			}
		})
	}

}
