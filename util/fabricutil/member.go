// Copyright 2016 Kai Chen <281165273@qq.com> (@grapebaba)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fabricutil

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Member struct {
	Name string
	// Kubernetes namespace this member runs in.
	Namespace string

	SecretName string

	ConfigName string

	OrgMSPId string
}

func (m *Member) fqdn() string {
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", m.Name, clusterNameFromMemberName(m.Name), m.Namespace)
}

type MemberSet map[string]*Member

func NewMemberSet() MemberSet {
	return MemberSet{}
}

func (m *Member) PeerAddr() string {
	return fmt.Sprintf("http://%s:7051", m.fqdn())
}

// the set of all members of s1 that are not members of s2
func (ms MemberSet) Diff(other MemberSet) MemberSet {
	diff := MemberSet{}
	for n, m := range ms {
		if _, ok := other[n]; !ok {
			diff[n] = m
		}
	}
	return diff
}

// IsEqual tells whether two member sets are equal by checking
// - they have the same set of members and member equality are judged by Name only.
func (ms MemberSet) IsEqual(other MemberSet) bool {
	if ms.Size() != other.Size() {
		return false
	}
	for n := range ms {
		if _, ok := other[n]; !ok {
			return false
		}
	}
	return true
}

func (ms MemberSet) Size() int {
	return len(ms)
}

func (ms MemberSet) String() string {
	var mstring []string

	for m := range ms {
		mstring = append(mstring, m)
	}
	return strings.Join(mstring, ",")
}

func (ms MemberSet) PickOne() *Member {
	for _, m := range ms {
		return m
	}
	panic("empty")
}

func (ms MemberSet) Add(m *Member) {
	ms[m.Name] = m
}

func (ms MemberSet) Remove(name string) {
	delete(ms, name)
}

func GetCounterFromMemberName(name string) (int, error) {
	i := strings.LastIndex(name, "-")
	if i == -1 || i+1 >= len(name) {
		return 0, fmt.Errorf("name (%s) does not contain '-' or anything after", name)
	}
	c, err := strconv.Atoi(name[i+1:])
	if err != nil {
		return 0, err
	}
	return c, nil
}

var validPeerURL = regexp.MustCompile(`^\w+:\/\/[\w\.\-]+(:\d+)?$`)

func MemberNameFromPeerURL(pu string) (string, error) {
	// url.Parse has very loose validation. We do our own validation.
	if !validPeerURL.MatchString(pu) {
		return "", errors.New("invalid PeerURL format")
	}
	u, err := url.Parse(pu)
	if err != nil {
		return "", err
	}
	path := strings.Split(u.Host, ":")[0]
	name := strings.Split(path, ".")[0]
	return name, err
}

func CreateMemberName(clusterName string, member int) string {
	return fmt.Sprintf("%s-peer%04d", clusterName, member)
}

func CreateMemberSecretName(clusterName string, member int) string {
	return fmt.Sprintf("%s-secret%04d", clusterName, member)
}

func CreateMemberConfigName(clusterName string, member int) string {
	return fmt.Sprintf("%s-config%04d", clusterName, member)
}

func clusterNameFromMemberName(mn string) string {
	i := strings.LastIndex(mn, "-")
	if i == -1 {
		panic(fmt.Sprintf("unexpected member name: %s", mn))
	}
	return mn[:i]
}
