// Secure is an http middleware for Go that facilitates some quick security wins.
//
// package main
//
// import (
//   "net/http"
//
//   "github.com/unrolled/secure"
// )
//
// func myApp(w http.ResponseWriter, r *http.Request) {
//   w.Write([]byte("Hello world!"))
// }
//
// func main() {
//   myHandler := http.HandlerFunc(myApp)
//
//   secureMiddleware := secure.New(secure.Options{
//     AllowedHosts: []string{"www.example.com", "sub.example.com"},
//     SSLRedirect:  true,
//   })
//
//   app := secureMiddleware.Handler(myHandler)
//   http.ListenAndServe("0.0.0.0:3000", app)
// }

package secure

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	stsHeader           = "Strict-Transport-Security"
	stsSubdomainString  = "; includeSubdomains"
	frameOptionsHeader  = "X-Frame-Options"
	frameOptionsValue   = "DENY"
	contentTypeHeader   = "X-Content-Type-Options"
	contentTypeValue    = "nosniff"
	xssProtectionHeader = "X-XSS-Protection"
	xssProtectionValue  = "1; mode=block"
	cspHeader           = "Content-Security-Policy"
)

func defaultBadHostHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Bad Host", http.StatusInternalServerError)
}

// Options is a struct for specifying configuration options for the secure.Secure middleware.
type Options struct {
	// AllowedHosts is a list of fully qualified domain names that are allowed. Default is empty list, which allows any and all host names.
	AllowedHosts []string
	// If SSLRedirect is set to true, then only allow https requests. Default is false.
	SSLRedirect bool
	// If SSLTemporaryRedirect is true, the a 302 will be used while redirecting. Default is false (301).
	SSLTemporaryRedirect bool
	// SSLHost is the host name that is used to redirect http requests to https. Default is "", which indicates to use the same host.
	SSLHost string
	// SSLProxyHeaders is set of header keys with associated values that would indicate a valid https request. Useful when using Nginx: `map[string]string{"X-Forwarded-Proto": "https"}`. Default is blank map.
	SSLProxyHeaders map[string]string
	// STSSeconds is the max-age of the Strict-Transport-Security header. Default is 0, which would NOT include the header.
	STSSeconds int64
	// If STSIncludeSubdomains is set to true, the `includeSubdomains` will be appended to the Strict-Transport-Security header. Default is false.
	STSIncludeSubdomains bool
	// If FrameDeny is set to true, adds the X-Frame-Options header with the value of `DENY`. Default is false.
	FrameDeny bool
	// CustomFrameOptionsValue allows the X-Frame-Options header value to be set with a custom value. This overrides the FrameDeny option.
	CustomFrameOptionsValue string
	// If ContentTypeNosniff is true, adds the X-Content-Type-Options header with the value `nosniff`. Default is false.
	ContentTypeNosniff bool
	// If BrowserXssFilter is true, adds the X-XSS-Protection header with the value `1; mode=block`. Default is false.
	BrowserXssFilter bool
	// ContentSecurityPolicy allows the Content-Security-Policy header value to be set with a custom value. Default is "".
	ContentSecurityPolicy string
	// When developing, the AllowedHosts, SSL, and STS options can cause some unwanted effects. Usually testing happens on http, not https, and on localhost, not your production domain... so set this to true for dev environment.
	// If you would like your development environment to mimic production with complete Host blocking, SSL redirects, and STS headers, leave this as false. Default if false.
	IsDevelopment bool
}

// Secure is a middleware that helps setup a few basic security features. A single secure.Options struct can be
// provided to configure which features should be enabled, and the ability to override a few of the default values.
type Secure struct {
	// Customize Secure with an Options struct.
	opt Options

	// Handlers for when an error occurs (ie bad host).
	badHostHandler http.Handler
}

// New constructs a new Secure instance with supplied options.
func New(options Options) *Secure {
	return &Secure{
		opt:            options,
		badHostHandler: http.HandlerFunc(defaultBadHostHandler),
	}
}

// Handler implements the http.HandlerFunc for integration with the standard net/http lib.
func (s *Secure) Handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Let secure process the request. If it returns an error,
		// that indicates the request should not continue.
		err := s.process(w, r)

		// If there was an error, do not continue.
		if err != nil {
			return
		}

		h.ServeHTTP(w, r)
	})
}

func (s *Secure) process(w http.ResponseWriter, r *http.Request) error {
	// Allowed hosts check.
	if len(s.opt.AllowedHosts) > 0 && !s.opt.IsDevelopment {
		isGoodHost := false
		for _, allowedHost := range s.opt.AllowedHosts {
			if strings.EqualFold(allowedHost, r.Host) {
				isGoodHost = true
				break
			}
		}

		if !isGoodHost {
			s.badHostHandler.ServeHTTP(w, r)
			return fmt.Errorf("Bad host name: %s", r.Host)
		}
	}

	// SSL check.
	if s.opt.SSLRedirect && s.opt.IsDevelopment == false {
		isSSL := false
		if strings.EqualFold(r.URL.Scheme, "https") || r.TLS != nil {
			isSSL = true
		} else {
			for k, v := range s.opt.SSLProxyHeaders {
				if r.Header.Get(k) == v {
					isSSL = true
					break
				}
			}
		}

		if isSSL == false {
			url := r.URL
			url.Scheme = "https"
			url.Host = r.Host

			if len(s.opt.SSLHost) > 0 {
				url.Host = s.opt.SSLHost
			}

			status := http.StatusMovedPermanently
			if s.opt.SSLTemporaryRedirect {
				status = http.StatusTemporaryRedirect
			}

			http.Redirect(w, r, url.String(), status)
			return fmt.Errorf("Redirecting to HTTPS")
		}
	}

	// Strict Transport Security header.
	if s.opt.STSSeconds != 0 && !s.opt.IsDevelopment {
		stsSub := ""
		if s.opt.STSIncludeSubdomains {
			stsSub = stsSubdomainString
		}

		w.Header().Add(stsHeader, fmt.Sprintf("max-age=%d%s", s.opt.STSSeconds, stsSub))
	}

	// Frame Options header.
	if len(s.opt.CustomFrameOptionsValue) > 0 {
		w.Header().Add(frameOptionsHeader, s.opt.CustomFrameOptionsValue)
	} else if s.opt.FrameDeny {
		w.Header().Add(frameOptionsHeader, frameOptionsValue)
	}

	// Content Type Options header.
	if s.opt.ContentTypeNosniff {
		w.Header().Add(contentTypeHeader, contentTypeValue)
	}

	// XSS Protection header.
	if s.opt.BrowserXssFilter {
		w.Header().Add(xssProtectionHeader, xssProtectionValue)
	}

	// Content Security Policy header.
	if len(s.opt.ContentSecurityPolicy) > 0 {
		w.Header().Add(cspHeader, s.opt.ContentSecurityPolicy)
	}

	return nil
}

// SetBadHostHandler sets the handler to call when secure rejects the host name.
func (s *Secure) SetBadHostHandler(handler http.Handler) {
	s.badHostHandler = handler
}

// HandlerFuncWithNext is a special implementation for Negroni, but could be used elsewhere.
func (s *Secure) HandlerFuncWithNext(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	err := s.process(w, r)

	// If there was an error, do not continue.
	if err != nil {
		return
	}

	if next != nil {
		next(w, r)
	}
}
