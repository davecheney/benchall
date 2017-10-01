// Copyright 2014 The Macaron Authors
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package cache

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func Test_MemoryCacher(t *testing.T) {
	Convey("Test memory cache adapter", t, func() {
		testAdapter(Options{
			Interval: 2,
		})
	})
}
