package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/russellcardullo/go-pingdom/pingdom"
)

var (
	waitSeconds int
	port        int
	apiKey      string

	pingdomUp = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pingdom_up",
		Help: "Whether the last pingdom scrape was successful (1: up, 0: down)",
	})

	pingdomCheckStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pingdom_check_status",
		Help: "The current status of the check (0: up, 1: unconfirmed_down, 2: down, -1: paused, -2: unknown)",
	}, []string{"id", "name", "hostname", "resolution", "paused", "tags"})

	pingdomCheckResponseTime = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pingdom_check_response_time",
		Help: "The response time of last test in milliseconds",
	}, []string{"id", "name", "hostname", "resolution", "paused", "tags"})

	pingdomCheckState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pingdom_check_state",
		Help: "The current state of the check (1: up, 0: down)",
	}, []string{"hostname"})
)

func main() {
	waitSeconds = getenvInt("WAIT_SECONDS", 10)
	port = getenvInt("PORT", 8000)

	apiKey = os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("apikey must not be empty")
	}

	prometheus.MustRegister(pingdomUp)
	prometheus.MustRegister(pingdomCheckStatus)
	prometheus.MustRegister(pingdomCheckState)
	prometheus.MustRegister(pingdomCheckResponseTime)

	client, err := pingdom.NewClientWithConfig(pingdom.ClientConfig{
		APIToken: apiKey,
	})
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		var oldCheckMetrics map[int]prometheus.Labels
		for {
			params := map[string]string{
				"include_tags": "true",
			}
			checks, err := client.Checks.List(params)
			if err != nil {
				log.Println("Error getting checks ", err)
				pingdomUp.Set(0)

				sleep()
				continue
			}
			pingdomUp.Set(1)

			checkMetrics := make(map[int]prometheus.Labels)
			for _, check := range checks {
				id := strconv.Itoa(check.ID)

				var status, state float64
				state = 0
				switch check.Status {
				case "unknown":
					status = -2
				case "paused":
					status = -1
				case "up":
					status = 0
					state = 1
				case "unconfirmed_down":
					status = 1
				case "down":
					status = 2
				default:
					status = 100
				}

				resolution := strconv.Itoa(check.Resolution)

				paused := strconv.FormatBool(check.Paused)
				// Pingdom library doesn't report paused correctly,
				// so calculate it off the status.
				if check.Status == "paused" {
					paused = "true"
				}

				var tagsRaw []string
				for _, tag := range check.Tags {
					tagsRaw = append(tagsRaw, tag.Name)
				}
				tags := strings.Join(tagsRaw, ",")

				labels := map[string]string{
					"id":         id,
					"name":       check.Name,
					"hostname":   check.Hostname,
					"resolution": resolution,
					"paused":     paused,
					"tags":       tags,
				}

				pingdomCheckStatus.With(labels).Set(status)
				pingdomCheckState.With(map[string]string{"hostname": check.Hostname}).Set(state)
				pingdomCheckResponseTime.With(labels).Set(float64(check.LastResponseTime))

				checkMetrics[check.ID] = labels
			}

			for id, oldLabels := range oldCheckMetrics {
				if labels, found := checkMetrics[id]; !found || !reflect.DeepEqual(oldLabels, labels) {
					pingdomCheckStatus.Delete(oldLabels)
					pingdomCheckResponseTime.Delete(oldLabels)
				}
			}

			oldCheckMetrics = checkMetrics

			sleep()
		}
	}()

	go func() {
		intChan := make(chan os.Signal, 1)
		termChan := make(chan os.Signal, 1)

		signal.Notify(intChan, syscall.SIGINT)
		signal.Notify(termChan, syscall.SIGTERM)

		select {
		case <-intChan:
			log.Print("Received SIGINT, exiting")
			os.Exit(0)
		case <-termChan:
			log.Print("Received SIGTERM, exiting")
			os.Exit(0)
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "")
	})
	http.Handle("/metrics", promhttp.Handler())

	log.Print("Listening on port ", port)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func sleep() {
	time.Sleep(time.Second * time.Duration(waitSeconds))
}

func getenvInt(k string, defaultValue int) int {
	valueString := os.Getenv(k)
	value, err := strconv.Atoi(valueString)
	if err != nil {
		return defaultValue
	}
	return value
}
