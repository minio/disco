// This file is part of Disco
// Copyright (c) 2020 MinIO, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/miekg/dns"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"log"
	"os"
	"strconv"

	"github.com/lixiangzhong/dnsutil"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/jsonpath"
)

// storage of the records the DNS will server
var records = map[string]string{}

// pod label
const DnsPodLabel = "io.min.disco"

// dig instance
var dig dnsutil.Dig

// parseDNSQuery parses a query for the DNS service and replies
func parseDNSQuery(m *dns.Msg) {
	for _, q := range m.Question {
		switch q.Qtype {
		case dns.TypeA:
			ip := records[q.Name]
			if ip != "" {
				rr, err := dns.NewRR(fmt.Sprintf("%s 5 A %s", q.Name, ip))
				if err == nil {
					m.Answer = append(m.Answer, rr)
				}
				log.Printf("Query for %s ✔\n", q.Name)
			} else {
				a, _ := dig.A(q.Name)
				if len(a) > 0 {
					rr, err := dns.NewRR(a[0].String())
					if err == nil {
						m.Answer = append(m.Answer, rr)
					}
					log.Printf("Query for %s 📒\n", q.Name)
				}
			}
		}
	}
}

func handleDnsRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false

	switch r.Opcode {
	case dns.OpcodeQuery:
		parseDNSQuery(m)
	}

	w.WriteMsg(m)
}

func watchPods(clientSet *kubernetes.Clientset) {
	//now listen for  pods
	log.Println("Starting Disco Informer")
	// informer factory
	doneCh := make(chan struct{})
	factory := informers.NewSharedInformerFactory(clientSet, 0)

	podInformer := factory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*v1.Pod)
			// monitor for pods with io.min.disco annotation
			if pod.Status.PodIP != "" {
				if labelQuery, ok := pod.ObjectMeta.Annotations[DnsPodLabel]; ok {
					domain := parseAnnotation(labelQuery, pod)
					log.Printf("ADD %s (%s)", domain, pod.Status.PodIP)
					records[domain] = pod.Status.PodIP
				}
			}
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			pod := newObj.(*v1.Pod)
			// monitor for pods with io.min.disco annotation
			if pod.Status.PodIP != "" {
				if labelQuery, ok := pod.ObjectMeta.Annotations[DnsPodLabel]; ok {
					domain := parseAnnotation(labelQuery, pod)
					if pod.ObjectMeta.DeletionTimestamp != nil {
						log.Printf("UDELETE %s (%s)", domain, pod.Status.PodIP)
						delete(records, domain)
					} else {
						log.Printf("UPDATE %s (%s) - %s - %s", domain, pod.Status.PodIP, pod.Status.Phase, pod.ObjectMeta.DeletionTimestamp)
					}
					records[domain] = pod.Status.PodIP
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			pod := obj.(*v1.Pod)
			// monitor for pods with io.min.disco annotation
			if labelQuery, ok := pod.ObjectMeta.Annotations[DnsPodLabel]; ok {
				domain := parseAnnotation(labelQuery, pod)
				log.Printf("DELETE %s (%s)", domain, pod.Status.PodIP)
				delete(records, domain)
			}
		},
	})

	go podInformer.Run(doneCh)
	//block until the informer exits
	<-doneCh
	log.Println("informer closed")
}

// parseAnnotation parses the annotation and resolves the jsonspaths in against the pod
func parseAnnotation(query string, pod *v1.Pod) string {
	var re = regexp.MustCompile(`(?m)(\{([a-z._-]+)\})`)
	for _, match := range re.FindAllStringSubmatch(query, -1) {
		jPathExpr := match[0]
		jPath := jsonpath.New("ok")
		err := jPath.Parse(jPathExpr)
		if err != nil {
			continue
		}
		buf := new(bytes.Buffer)
		err = jPath.Execute(buf, pod)
		if err != nil {
			continue
		}
		out := buf.String()
		query = strings.Replace(query, match[0], out, -1)
	}
	domain := fmt.Sprintf("%s.", query)
	return domain
}

func main() {
	// attach request handler func
	dns.HandleFunc(".", handleDnsRequest)

	var config *rest.Config
	if os.Getenv("DEVELOPMENT") != "" {
		//when doing local development, mount k8s api via `kubectl proxy`
		config = &rest.Config{
			Host:            "http://localhost:8001",
			TLSClientConfig: rest.TLSClientConfig{Insecure: true},
			APIPath:         "/",
			BearerToken:     "eyJhbGciOiJSUzI1NiIsImtpZCI6InFETTJ6R21jMS1NRVpTOER0SnUwdVg1Q05XeDZLV2NKVTdMUnlsZWtUa28ifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6ImRldi1zYS10b2tlbi14eGxuaiIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUiOiJkZXYtc2EiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC51aWQiOiJmZDVhMzRjNy0wZTkwLTQxNTctYmY0Zi02Yjg4MzIwYWIzMDgiLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6ZGVmYXVsdDpkZXYtc2EifQ.woZ6Bmkkw-BMV-_UX0Y-S_Lkb6H9zqKZX2aNhyy7valbYIZfIzrDqJYWV9q2SwCP20jBfdsDS40nDcMnHJPE5jZHkTajAV6eAnoq4EspRqORtLGFnVV-JR-okxtvhhQpsw5MdZacJk36ED6Hg8If5uTOF7VF5r70dP7WYBMFiZ3HSlJBnbu7QoTKFmbJ1MafsTQ2RBA37IJPkqi3OHvPadTux6UdMI8LlY7bLkZkaryYR36kwIzSqsYgsnefmm4eZkZzpCeyS9scm9lPjeyQTyCAhftlxfw8m_fsV0EDhmybZCjgJi4R49leJYkHdpnCSkubj87kJAbGMwvLhMhFFQ",
		}
	} else {
		var err error
		config, err = rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}

	}
	//Create a new client to interact with cluster and leave if it doesn't work
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	// get the cluster internal DNS IP
	svc, err := clientSet.
		CoreV1().
		Services("kube-system").
		Get(context.Background(), "kube-dns", metav1.GetOptions{})

	dig.SetDNS(svc.Spec.ClusterIP)

	// start server
	port := 53
	server := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}
	log.Printf("Starting at %d\n", port)
	go watchPods(clientSet)
	err = server.ListenAndServe()
	defer server.Shutdown()
	if err != nil {
		log.Fatalf("Failed to start server: %s\n ", err.Error())
	}
}
