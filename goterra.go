package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"github.com/gorilla/mux"

	terraConfig "github.com/osallou/goterra/lib/config"
	terraDb "github.com/osallou/goterra/lib/db"
)

var Version string

// HomeHandler manages base entrypoint
var HomeHandler = func(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{"version": Version, "message": "ok"}
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// DeploymentData represents data sent to update a deployment value
type DeploymentData struct {
	Key   string
	Value string
}

// Claims contains JWT claims
type Claims struct {
	Deployment string `json:"deployment"`
	jwt.StandardClaims
}

// CheckTokenForDeployment checks JWT token and token maps to current deployment
func CheckTokenForDeployment(authToken string, deployment string) bool {
	// TODO
	config := terraConfig.LoadConfig()

	tokenStr := strings.Replace(authToken, "Bearer", "", -1)
	tokenStr = strings.TrimSpace(tokenStr)
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.Secret), nil
	})
	if err != nil || !token.Valid || claims.Audience != "goterra/deployment" {
		fmt.Printf("Token error: %v\n", err)
		return false
	}
	if claims.Deployment != deployment {
		fmt.Printf("Trying to access a different deployment %s from %s\n", deployment, claims.Deployment)
		return false
	}
	return true
}

// DeploymentHandler creates a deployment
var DeploymentHandler = func(w http.ResponseWriter, r *http.Request) {
	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	id := uuid.New()
	idStr := id.String()
	t := time.Now()
	err := dbHandler.Client.HSet(dbHandler.Prefix+":depl:"+idStr, "ts", t.Unix()).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		respError := map[string]interface{}{"message": "failed to generate deployment id"}
		json.NewEncoder(w).Encode(respError)
		return
	}
	mySigningKey := []byte(config.Secret)

	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		Deployment: idStr,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
			Audience:  "goterra/deployment",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(mySigningKey)
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"url": config.URL, "id": idStr, "token": tokenString}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentDeleteHandler deletes a deployment info
var DeploymentDeleteHandler = func(w http.ResponseWriter, r *http.Request) {
	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)

	vars := mux.Vars(r)

	if !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not authorized"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	idStr := vars["deployment"]
	err := dbHandler.Client.Del(dbHandler.Prefix + ":depl:" + idStr).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		respError := map[string]interface{}{"message": "failed to delete deployment id"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	// w.WriteHeader(http.StatusNotFound)
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"message": "deleted"}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentUpdateHandler updates a deployment
var DeploymentUpdateHandler = func(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	data := &DeploymentData{}
	err := json.NewDecoder(r.Body).Decode(data)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "failed to decode message"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	if !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not authorized"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	t := time.Now()
	err = dbHandler.Client.HSet(dbHandler.Prefix+":depl:"+vars["deployment"], "lastts", t.Unix()).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "failed to update deployment"}
		json.NewEncoder(w).Encode(respError)
		return
	}
	err = dbHandler.Client.HSet(dbHandler.Prefix+":depl:"+vars["deployment"], data.Key, data.Value).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "failed to update deployment"}
		json.NewEncoder(w).Encode(respError)
		return
	}
	// w.WriteHeader(http.StatusNotFound)
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"message": "done"}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentGetHandler returns a deployment value, not found if not defined
var DeploymentGetHandler = func(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	if !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not authorized"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	value, err := dbHandler.Client.HGet(dbHandler.Prefix+":depl:"+vars["deployment"], vars["key"]).Result()
	if err != nil || value == "" {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not found"}
		json.NewEncoder(w).Encode(respError)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"key": vars["key"], "value": value}
	json.NewEncoder(w).Encode(resp)
}

// DeploymentGetKeysHandler returns available variables for deployment
var DeploymentGetKeysHandler = func(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	if !CheckTokenForDeployment(r.Header.Get("Authorization"), vars["deployment"]) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not authorized"}
		json.NewEncoder(w).Encode(respError)
		return
	}

	config := terraConfig.LoadConfig()
	dbHandler := terraDb.NewClient(config)
	values, err := dbHandler.Client.HGetAll(dbHandler.Prefix + ":depl:" + vars["deployment"]).Result()
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Add("Content-Type", "application/json")
		respError := map[string]interface{}{"message": "not found"}
		json.NewEncoder(w).Encode(respError)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	resp := map[string]interface{}{"deployment": values}
	json.NewEncoder(w).Encode(resp)
}

func main() {

	config := terraConfig.LoadConfig()

	r := mux.NewRouter()
	r.HandleFunc("/", HomeHandler).Methods("GET")
	r.HandleFunc("/deployment", DeploymentHandler).Methods("POST")
	r.HandleFunc("/deployment/{deployment}", DeploymentUpdateHandler).Methods("PUT")
	r.HandleFunc("/deployment/{deployment}", DeploymentGetKeysHandler).Methods("GET")
	r.HandleFunc("/deployment/{deployment}", DeploymentDeleteHandler).Methods("DELETE")
	r.HandleFunc("/deployment/{deployment}/{key}", DeploymentGetHandler).Methods("GET")

	srv := &http.Server{
		Handler: r,
		Addr:    fmt.Sprintf("%s:%d", config.Web.Listen, config.Web.Port),
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())

}
