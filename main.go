package main

import (
	"encoding/json"
	"net/http"
	"time"

	"context"
	"fmt"

	"crypto/tls"

	"charm.land/log/v2"
	LOG "charm.land/log/v2"
	J "github.com/Ceald1/jellyfin-metrics/api"
	env "github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	ctx = context.Background()
)

func withToken(token string) J.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token="%s"`, token))
		return nil
	}
}

func NewJellyfinClient(host, apikey string) (c *J.ClientWithResponses, err error) {
	var authInfo J.AuthenticationInfo
	tr := http.DefaultTransport.(*http.Transport).Clone() // keeps proxy, timeouts, HTTP/2 etc.
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	httpClient := &http.Client{
		Transport: tr,
		Timeout:   15 * time.Second,
	}
	authInfo.AccessToken = apikey
	c, err = J.NewClientWithResponses(host,
		J.WithHTTPClient(httpClient),
		J.WithRequestEditorFn(withToken(apikey)),
	)

	if err != nil {
		return
	}
	_, err = c.GetSessions(ctx, &J.GetSessionsParams{}) // test
	return
}

// get active sessions
func GetSessions(c *J.ClientWithResponses) (sessions []J.SessionInfoDto, err error) {
	resp, err := c.GetSessionsWithResponse(ctx, &J.GetSessionsParams{})
	if err != nil {
		return
	}
	err = json.Unmarshal(resp.Body, &sessions)

	return
}

func GetStreams(c *J.ClientWithResponses, g *prometheus.GaugeVec) {
	resp, err := GetSessions(c)
	if err != nil {
		log.Warn(err.Error())
		return
	}
	g.Reset()
	for _, s := range resp {
		if s.NowPlayingItem == nil {
			continue
		}
		g.WithLabelValues(s.UserName, string(*s.NowPlayingItem.Type), s.NowPlayingItem.Name)
	}
}

type Metrics struct {
	ActiveSessions prometheus.Counter
	ActiveStreams  *prometheus.GaugeVec
}

func register(reg prometheus.Registerer, m *Metrics, apiKey, host string) {
	newClient, err := NewJellyfinClient(host, apiKey)
	if err != nil {
		LOG.Fatal(err.Error())
	}
	LOG.Info("made a new client successfully")
	promauto.With(reg).NewCounterFunc(prometheus.CounterOpts{
		Name: "jellyfin_active_sessions_total",
		Help: "total number of active sessions on jellyfin",
	}, func() float64 {
		sessions, err := GetSessions(newClient)
		if err != nil {
			return 0
		}
		isActive := 0
		for _, s := range sessions {
			if s.IsActive != false {
				isActive++
			}
		}
		return float64(isActive)
	})
	streams := promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "jellyfin_now_playing",
		Help: "now playing",
	}, []string{"user", "media_type", "now_playing"})
	go func() {
		for {
			GetStreams(newClient, streams)
			time.Sleep(15 * time.Second)
		}
	}()

}

func main() {
	envVars, err := env.Read(".env")
	if err != nil {
		LOG.Fatal(err.Error())
	}

	jel := &Metrics{}
	reg := prometheus.NewRegistry()
	register(reg, jel, envVars["API_KEY"], envVars["HOST"])

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	http.ListenAndServe(":2112", nil)
}
