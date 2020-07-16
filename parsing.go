// This file is part of MinIO Disco
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
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/jsonpath"
	"regexp"
	"strings"
)

// parsePodAnnotation parses the annotation and resolves the jsonspaths in against the pod
func parsePodAnnotation(query string, pod *v1.Pod) string {
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

// parseSvcAnnotation parses the annotation and resolves the jsonspaths in against the service
func parseSvcAnnotation(query string, svc *v1.Service) string {
	var re = regexp.MustCompile(`(?m)(\{([a-z._-]+)\})`)
	for _, match := range re.FindAllStringSubmatch(query, -1) {
		jPathExpr := match[0]
		jPath := jsonpath.New("ok")
		err := jPath.Parse(jPathExpr)
		if err != nil {
			continue
		}
		buf := new(bytes.Buffer)
		err = jPath.Execute(buf, svc)
		if err != nil {
			continue
		}
		out := buf.String()
		query = strings.Replace(query, match[0], out, -1)
	}
	domain := fmt.Sprintf("%s.", query)
	return domain
}
