package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"

	"encoding/json"

	"github.com/Bashayr29/k8s-admission-controller/api/v1alpha1"
	"github.com/rs/zerolog/log"
	admission "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecFactory  = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecFactory.UniversalDeserializer()
)

// add kind AdmissionReview in scheme
func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admission.AddToScheme(runtimeScheme)

}

type admitv1Func func(admission.AdmissionReview) *admission.AdmissionResponse

type admitHandler struct {
	v1 admitv1Func
}

func AdmitHandler(f admitv1Func) admitHandler {
	return admitHandler{
		v1: f,
	}
}

// serve handles the http portion of a request prior to handing to an admit
// function
func serve(w http.ResponseWriter, r *http.Request, admit admitHandler) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		log.Error().Msgf("contentType=%s, expect application/json", contentType)
		return
	}

	log.Info().Msgf("handling request: %s", body)
	var responseObj runtime.Object
	if obj, gvk, err := deserializer.Decode(body, nil, nil); err != nil {
		msg := fmt.Sprintf("Request could not be decoded: %v", err)
		log.Error().Msg(msg)
		http.Error(w, msg, http.StatusBadRequest)
		return

	} else {
		requestedAdmissionReview, ok := obj.(*admission.AdmissionReview)
		if !ok {
			log.Error().Msgf("Expected v1.AdmissionReview but got: %T", obj)
			return
		}
		responseAdmissionReview := &admission.AdmissionReview{}
		responseAdmissionReview.SetGroupVersionKind(*gvk)
		responseAdmissionReview.Response = admit.v1(*requestedAdmissionReview)
		responseAdmissionReview.Response.UID = requestedAdmissionReview.Request.UID
		responseObj = responseAdmissionReview

	}
	log.Info().Msgf("sending response: %v", responseObj)
	respBytes, err := json.Marshal(responseObj)
	if err != nil {
		log.Err(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		log.Err(err)
	}
}

func serveMutate(w http.ResponseWriter, r *http.Request) {
	serve(w, r, AdmitHandler(mutate))
}
func serveValidate(w http.ResponseWriter, r *http.Request) {
	serve(w, r, AdmitHandler(validate))
}

// adds prefix 'prod' to every incoming Deployment, example: prod-apps
func mutate(ar admission.AdmissionReview) *admission.AdmissionResponse {
	log.Info().Msgf("mutating deployments")

	memcachedResource := metav1.GroupVersionResource{Group: "cache.example.com", Version: "v1alpha1", Resource: "memcacheds"}

	if ar.Request.Resource != memcachedResource {
		log.Error().Msgf("expect resource to be %s", memcachedResource)
		return nil
	}

	raw := ar.Request.Object.Raw
	memcached := v1alpha1.Memcached{}

	if _, _, err := deserializer.Decode(raw, nil, &memcached); err != nil {
		log.Err(err)
		return &admission.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	log.Info().Msgf("Logging Memcached %s", memcached)

	if memcached.Spec.Size == 0 {
		return &admission.AdmissionResponse{
			Result: &metav1.Status{
				Message: "size needs to be > 0",
			},
		}
	}

	return &admission.AdmissionResponse{
		Allowed: true, Result: &metav1.Status{
			Message: "Testing mutation hook",
		},
	}
}

// verify if a Deployment has the 'prod' prefix name
func validate(ar admission.AdmissionReview) *admission.AdmissionResponse {
	log.Info().Msgf("validating deployments")
	return &admission.AdmissionResponse{
		Allowed: true, Result: &metav1.Status{
			Message: "Testing validating hook",
		},
	}
}

func main() {
	var tlsKey, tlsCert string
	flag.StringVar(&tlsKey, "tlsKey", "/etc/certs/tls.key", "Path to the TLS key")
	flag.StringVar(&tlsCert, "tlsCert", "/etc/certs/tls.crt", "Path to the TLS certificate")
	flag.Parse()
	http.HandleFunc("/mutate", serveMutate)
	http.HandleFunc("/validate", serveValidate)
	log.Info().Msg("Server started ...")
	log.Fatal().Err(http.ListenAndServeTLS(":9443", tlsCert, tlsKey, nil)).Msg("webhook server exited")
}
