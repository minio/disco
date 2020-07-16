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
	"context"
	"flag"
	"fmt"
	"github.com/miekg/dns"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"

	"log"
	"os"
	"strconv"

	"github.com/lixiangzhong/dnsutil"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// version disco version string set with -X main.version=1.0.0
var version string

// storage of the singleRecords the DNS will server
var singleRecords = map[string]string{
	// this is a domain to validate the presence of Disco
	"probe.minio.local.": "0.0.0.0",
}

// storage of the starRecords the DNS will serve, ie: tenant.minio.local, bucket.tenant.minio.local will resolve
// to the same IP
var starRecords = map[string]string{}

// pod label
const DiscoAnnotation = "disco.min.io"

// dig instance
var dig dnsutil.Dig

// parseDNSQuery parses a query for the DNS service and replies
func parseDNSQuery(m *dns.Msg) {
	for _, q := range m.Question {
		switch q.Qtype {
		case dns.TypeA:
			// Check if we match an exact record
			ip := singleRecords[q.Name]
			if ip != "" {
				rr, err := dns.NewRR(fmt.Sprintf("%s 5 A %s", q.Name, ip))
				if err == nil {
					m.Answer = append(m.Answer, rr)
				}
				log.Printf("Query for %s ✔\n", q.Name)
				return
			}
			// check if the query is a subdomain of a starRecord
			for domain, ip := range starRecords {
				if strings.HasSuffix(q.Name, domain) {
					rr, err := dns.NewRR(fmt.Sprintf("%s 5 A %s", q.Name, ip))
					if err == nil {
						m.Answer = append(m.Answer, rr)
					}
					log.Printf("Query for %s ✔\n", q.Name)
					return
				}
			}
			// if we found no match for this query, forward to kube-dns
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

// handleDNSRequest handles DNS queries to Disco
func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false

	switch r.Opcode {
	case dns.OpcodeQuery:
		parseDNSQuery(m)
	}

	w.WriteMsg(m)
}

// watchPods watches for all the pods being created/updated/deleted
func watchPods(clientSet *kubernetes.Clientset) {
	//now listen for  pods
	log.Println("Starting Disco Pod Informer")
	// informer factory
	doneCh := make(chan struct{})
	factory := informers.NewSharedInformerFactory(clientSet, 0)

	podInformer := factory.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*v1.Pod)
			// monitor for pods with disco.min.io annotation
			if pod.Status.PodIP != "" {
				if labelQuery, ok := pod.ObjectMeta.Annotations[DiscoAnnotation]; ok {
					domain := parsePodAnnotation(labelQuery, pod)
					log.Printf("ADD %s (%s)", domain, pod.Status.PodIP)
					singleRecords[domain] = pod.Status.PodIP
				}
			}
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			pod := newObj.(*v1.Pod)
			// monitor for pods with disco.min.io annotation
			if pod.Status.PodIP != "" {
				if labelQuery, ok := pod.ObjectMeta.Annotations[DiscoAnnotation]; ok {
					domain := parsePodAnnotation(labelQuery, pod)
					if pod.ObjectMeta.DeletionTimestamp != nil {
						log.Printf("UDELETE %s (%s)", domain, pod.Status.PodIP)
						delete(singleRecords, domain)
					} else {
						log.Printf("UPDATE %s (%s) - %s - %s", domain, pod.Status.PodIP, pod.Status.Phase, pod.ObjectMeta.DeletionTimestamp)
					}
					singleRecords[domain] = pod.Status.PodIP
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			pod := obj.(*v1.Pod)
			// monitor for pods with disco.min.io annotation
			if labelQuery, ok := pod.ObjectMeta.Annotations[DiscoAnnotation]; ok {
				domain := parsePodAnnotation(labelQuery, pod)
				log.Printf("DELETE %s (%s)", domain, pod.Status.PodIP)
				delete(singleRecords, domain)
			}
		},
	})

	go podInformer.Run(doneCh)
	//block until the informer exits
	<-doneCh
	log.Println("pod informer closed")
}

// watchSvcs waches for all the services being created/updated/deleted
func watchSvcs(clientSet *kubernetes.Clientset) {
	//now listen for  pods
	log.Println("Starting Disco Service Informer")
	// informer factory
	doneCh := make(chan struct{})
	factory := informers.NewSharedInformerFactory(clientSet, 0)

	svcInformer := factory.Core().V1().Services().Informer()
	svcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*v1.Service)
			// wait for the svc to have a clusterIP
			if svc.Spec.ClusterIP != "" {
				// monitor for services with disco.min.io annotation
				if labelQuery, ok := svc.ObjectMeta.Annotations[DiscoAnnotation]; ok {
					domain := parseSvcAnnotation(labelQuery, svc)
					log.Printf("ADD SVC %s (%s)", domain, svc.Spec.ClusterIP)
					singleRecords[domain] = svc.Spec.ClusterIP
					starRecords[domain] = svc.Spec.ClusterIP
				}
			}

		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			svc := newObj.(*v1.Service)
			// wait for the svc to have a clusterIP
			if svc.Spec.ClusterIP != "" {
				// monitor for pods with disco.min.io annotation
				if labelQuery, ok := svc.ObjectMeta.Annotations[DiscoAnnotation]; ok {
					domain := parseSvcAnnotation(labelQuery, svc)
					if svc.ObjectMeta.DeletionTimestamp != nil {
						log.Printf("UDELETE SVC %s (%s)", domain, svc.Spec.ClusterIP)
						delete(singleRecords, domain)
					} else {
						log.Printf("UPDATE SVC %s (%s) - %s", domain, svc.Spec.ClusterIP, svc.ObjectMeta.DeletionTimestamp)
					}
					singleRecords[domain] = svc.Spec.ClusterIP
					starRecords[domain] = svc.Spec.ClusterIP
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			svc := obj.(*v1.Service)
			// monitor for pods with disco.min.io annotation
			if labelQuery, ok := svc.ObjectMeta.Annotations[DiscoAnnotation]; ok {
				domain := parseSvcAnnotation(labelQuery, svc)
				log.Printf("DELETE SVC %s (%s)", domain, svc.Spec.ClusterIP)
				delete(singleRecords, domain)
				delete(starRecords, domain)
			}
		},
	})

	go svcInformer.Run(doneCh)
	//block until the informer exits
	<-doneCh
	log.Println("service informer closed")
}

func main() {
	v := flag.Bool("v", false, "prints current disco version")
	flag.Parse()
	if *v {
		fmt.Println(version)
		os.Exit(0)
	}

	// attach request handler func
	dns.HandleFunc(".", handleDNSRequest)

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
	portStr := os.Getenv("DISCO_PORT")
	if portStr == "" {
		portStr = "53"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		panic(err)
	}
	server := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}
	log.Printf("Starting at %d\n", port)
	go watchPods(clientSet)
	go watchSvcs(clientSet)
	err = server.ListenAndServe()
	defer server.Shutdown()
	if err != nil {
		log.Fatalf("Failed to start server: %s\n ", err.Error())
	}
}
