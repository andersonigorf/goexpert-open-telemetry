package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.23.1"
)

const (
	ViaCepUrl     = "https://viacep.com.br/ws/%s/json/"
	WeatherApiUrl = "http://api.weatherapi.com/v1/current.json?key=%s&aqi=no&q=%s"

	MethodNotAllowed    = "method not allowed"
	UnprocessibleEntity = "invalid zipcode"
	InternalServerError = "error while searching for "
	InvalidJson         = "invalid json"
	NotFoundMessage     = "can not find zipcode"
)

type ZipCode struct {
	CEP string `json:"cep"`
}

type WeatherApi struct {
	Current struct {
		TempC float64 `json:"temp_c"`
	} `json:"current"`
}

type WeatherResponse struct {
	City  string  `json:"city"`
	TempC float64 `json:"temp_C"`
	TempF float64 `json:"temp_F"`
	TempK float64 `json:"temp_K"`
}

func init() {
	viper.AutomaticEnv()
}

func initProvider() {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(viper.GetString("OTEL_SERVICE_NAME")),
		),
	)
	if err != nil {
		log.Fatalf("failed to create resource: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, viper.GetString("OTEL_EXPORTER_OTLP_ENDPOINT"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("failed to create gRPC connection to collector: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		log.Fatalf("failed to create trace exporter: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
}

func main() {
	initProvider()

	http.HandleFunc("/weather", HandleRequest)
	fmt.Println("Starting web server on port" + viper.GetString("HTTP_PORT"))
	err := http.ListenAndServe(viper.GetString("HTTP_PORT"), nil)
	if err != nil {
		return
	}
}

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	tr := otel.Tracer(viper.GetString("OTEL_SERVICE_NAME"))
	ctx, span := tr.Start(context.Background(), viper.GetString("REQUEST_NAME_OTEL"))
	defer span.End()

	if r.Method != http.MethodPost {
		http.Error(w, MethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}

	var requestData ZipCode
	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		http.Error(w, InvalidJson, http.StatusBadRequest)
		return
	}

	validate := regexp.MustCompile(`^[0-9]{5}-?[0-9]{3}$`)
	if !validate.MatchString(requestData.CEP) {
		http.Error(w, UnprocessibleEntity, http.StatusUnprocessableEntity)
		return
	}

	city, err := searchCity(ctx, requestData.CEP)
	if err != nil {
		http.Error(w, NotFoundMessage, http.StatusNotFound)
		return
	}

	weather, err := searchWeather(ctx, city)
	if err != nil {
		http.Error(w, InternalServerError+"weather: "+err.Error(), http.StatusInternalServerError)
		return
	}

	weatherData := parseWeatherResponse(city, weather)

	weatherReturn := fmt.Sprintf("Weather in %s: %.1fC, %.1fF, %.1fK", weatherData.City, weatherData.TempC, weatherData.TempF, weatherData.TempK)

	span.SetAttributes(attribute.String("weather", weatherReturn))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(weatherData)
}

func parseWeatherResponse(city string, weather *WeatherApi) WeatherResponse {
	return WeatherResponse{
		City:  city,
		TempC: weather.Current.TempC,
		TempF: weather.Current.TempC*1.8 + 32,
		TempK: weather.Current.TempC + 273,
	}
}

func makeHTTPRequest(url string) (*http.Response, error) {
	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	log.Printf("Requesting URL: %s", url)
	resp, err := client.Get(url)

	return resp, err
}

func searchCity(ctx context.Context, cep string) (string, error) {
	tr := otel.Tracer(viper.GetString("OTEL_SERVICE_NAME"))
	_, span := tr.Start(ctx, viper.GetString("REQUEST_NAME_OTEL")+" - searchCity")
	span.SetAttributes(attribute.String("cep", cep))
	defer span.End()

	cepURL := fmt.Sprintf(ViaCepUrl, cep)

	resp, err := makeHTTPRequest(cepURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(InternalServerError + "city")
	}

	var result map[string]interface{}
	res, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	err = json.Unmarshal(res, &result)
	if err != nil {
		return "", err
	}

	if result["erro"] != nil {
		return "", fmt.Errorf(UnprocessibleEntity)
	}

	city, ok := result["localidade"].(string)
	if !ok {
		return "", fmt.Errorf(InternalServerError + "city")
	}

	return city, nil
}

func searchWeather(ctx context.Context, city string) (*WeatherApi, error) {
	tr := otel.Tracer(viper.GetString("OTEL_SERVICE_NAME"))
	_, span := tr.Start(ctx, viper.GetString("REQUEST_NAME_OTEL")+" - searchWeather")
	defer span.End()

	cityEscaped := url.QueryEscape(city)
	weatherApiURL := fmt.Sprintf(WeatherApiUrl, viper.GetString("WEATHER_API_KEY"), cityEscaped)

	span.SetAttributes(attribute.String("city", cityEscaped))

	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}

	log.Printf("Requesting URL: %s", weatherApiURL)
	resp, err := client.Get(weatherApiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(InternalServerError + "weather")
	}

	var weather WeatherApi
	res, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(res, &weather)
	if err != nil {
		return nil, err
	}

	return &weather, nil
}
