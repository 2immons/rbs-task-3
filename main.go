package main

import (
	"bytes"
	"context"
	"encoding/json"
	fileprocessing "file-explorer/server/fileprocessing"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"
)

const configFilePath string = "server.config.json" // путь к файлу конфигураци

type ServerConfig struct {
	Port          string `json:"port"`          // порт сервера
	ApachePostURL string `json:"apachePostURL"` // адрес запроса Apache
	ApacheURL     string `json:"apacheURL"`     // адрес Apache
}

type NodesGetResponseStruct struct {
	IsSucceed bool        `json:"serverIsSucceed"` // успешно ли обработан запрос
	ErrorText string      `json:"serverErrorText"` // текст ошибки
	Nodes     interface{} `json:"nodes"`           // вхождения в заданную запросом директорию
	LoadTime  float64     `json:"loadTime"`        // время обработки запроса
}

type StatsPostRequestStruct struct {
	ErrorText   string    `json:"serverErrorText"` // текст ошибки
	TotalSize   int64     `json:"totalSize"`       // общий вес всех директорий
	LoadTime    float64   `json:"loadTime"`        // время обработки запроса
	RequestDate time.Time `json:"date"`            // дата и время обработки
	RootPath    string    `json:"path"`            // корневой путь обработки
}

func main() {
	defer func() {
		r := recover()
		if r != nil {
			fmt.Printf("Остановка сервера из-за паники: %v.", r)
		}
	}()

	// создание корневого контекста для программы с функцией отмены
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// отлавливание сигнала прерывания работы и передача его в канал
	osSignalChan := make(chan os.Signal, 1)
	signal.Notify(osSignalChan, os.Interrupt)

	// горутина закрытия приложения с отменой корневого контекста
	go func() {
		<-osSignalChan
		fmt.Println("Получен сигнал остановки...")
		cancel()
	}()

	// чтение конфигурационных данных сервера
	serverConfig, err := readConfigFromFile(configFilePath)
	if err != nil {
		panic(err)
	}

	// инициализации сервера
	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", serverConfig.Port),
		Handler: http.DefaultServeMux,
	}

	http.Handle("/dist/", http.StripPrefix("/dist/", http.FileServer(http.Dir("./dist"))))
	http.Handle("/", http.StripPrefix("/", http.FileServer(http.Dir("./dist"))))

	http.HandleFunc("/nodes", handleGetNodesRequest)

	// горутина запуска сервера
	go func() {
		fmt.Printf("Запуск сервера на http://localhost:%s ...\n", serverConfig.Port)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			panic(err)
		}

		cancel()
	}()

	<-ctx.Done()

	fmt.Println("Остановка сервера...")
	err = server.Shutdown(ctx)
	if err != nil {
		fmt.Printf("Ошибка при остановке сервера %v\n", err)
	}
}

// readConfigFromFile получает кофигурационные данные из соответствующего файла и возвращает их
func readConfigFromFile(configFilePath string) (ServerConfig, error) {
	var config ServerConfig
	configFileContent, err := os.ReadFile(configFilePath)
	if err != nil {
		return config, err
	}

	err = json.Unmarshal(configFileContent, &config)
	if err != nil {
		return config, err
	}

	return config, nil
}

// getRequestParams получает параметры из запроса: путь корневой папки, поле сортировки, порядок сортировки
func getRequestParams(r *http.Request) (string, string, string, error) {
	srcPath := r.URL.Query().Get("path")
	sortField := r.URL.Query().Get("sortField")
	sortOrder := r.URL.Query().Get("sortOrder")
	if sortField == "" || sortOrder == "" {
		err := fmt.Errorf("переданы пустые параметры сортировки (поле и/или порядок)")
		return "", "", "", err
	}
	return srcPath, sortField, sortOrder, nil
}

// handleGetNodesRequest совершает обход директорий по указанному в запросе пути,
// получает информацию (имя, размер, тип: файл или папка и дата модификации) о каждом входящем в указанную директорию элементе (папке или файле)
// и отправляет её в формате JSON
func handleGetNodesRequest(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	responseData := NodesGetResponseStruct{
		IsSucceed: true,
		ErrorText: "",
		Nodes:     "",
		LoadTime:  0,
	}

	// получение параметров из запроса
	srcPath, sortField, sortOrder, err := getRequestParams(r)
	if err != nil {
		panic(err)
	}

	// получение отсортированного среза узлов файловой системы с информацией о них
	// по заданному пути, полю сортировки и порядку
	nodesSliceForJson, totalSize, srcPath, err := fileprocessing.GetNodesSliceForJson(srcPath, sortField, sortOrder)
	if err != nil {
		requestLoadTime := float64(time.Since(startTime).Seconds())

		w.WriteHeader(http.StatusOK)

		responseData.ErrorText = fmt.Sprintf("Внутренняя ошибка сервера: %v", err)
		responseData.IsSucceed = false
		responseData.LoadTime = requestLoadTime
		responseData.Nodes = "Нет данных"

		responseJsonFormat, err := json.Marshal(responseData)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(responseJsonFormat)
		return
	}

	// расчет времени выполнения работы функции
	requestLoadTime := float64(time.Since(startTime).Seconds())

	responseData.Nodes = nodesSliceForJson
	responseData.LoadTime = requestLoadTime

	// маршалинг ответа
	responseJsonFormat, err := json.Marshal(responseData)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// отправка ответа на клиент
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJsonFormat)

	// отправка данных о размере, времени загрузки, даты запроса и пути обхода на сервер Apache
	sendStatsToServer(totalSize, requestLoadTime, time.Now(), srcPath)
}

// sendStatsToServer отправляет полученный суммарный размер, время загрузки, текущую дату и время и путь,
// по которому был совершен обход, на сервер
func sendStatsToServer(totalSize int64, loadTime float64, dateTime time.Time, srcPath string) {

	// чтение конфигурационных данных из файла
	serverConfig, err := readConfigFromFile(configFilePath)
	if err != nil {
		panic(err)
	}

	requestData := StatsPostRequestStruct{
		ErrorText:   "",
		TotalSize:   totalSize,
		LoadTime:    loadTime,
		RequestDate: dateTime,
		RootPath:    srcPath,
	}

	// маршалинг запроса
	jsonRequestData, err := json.Marshal(requestData)
	if err != nil {
		return
	}

	client := &http.Client{}

	apacheServerReqAdrr := serverConfig.ApacheURL + serverConfig.ApachePostURL

	// отправка запроса
	req, err := http.NewRequest("POST", apacheServerReqAdrr, bytes.NewBuffer(jsonRequestData))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "applicaton/json")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// чтение ответа от сервера и вывод его в терминал
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	fmt.Println(string(responseBody))
}
