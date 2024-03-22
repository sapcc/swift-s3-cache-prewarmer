/*******************************************************************************
*
* Copyright 2021 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package main

import "testing"

func TestSortCommaSeparated(t *testing.T) {
	testCases := [][]string{
		// We test that:
		//- Inside each list, all elements are identical except for field sorting.
		//- But each two elements from different lists are different.
		{"foo,bar,qux", "foo,qux,bar", "bar,foo,qux", "bar,qux,foo", "qux,bar,foo", "qux,foo,bar"},
		{"foo,bar,", "foo,,bar", "bar,foo,", "bar,,foo", ",bar,foo", ",foo,bar"},
		{"foo,bar", "bar,foo"},
		{"foo,", ",foo"},
	}

	// inside each list in `testCases`, all elements are identical except for field sorting...
	for _, samples := range testCases {
		for _, reference := range samples {
			for _, input := range samples {
				sorted := sortCommaSeparatedLikeInReference(input, reference)
				if sorted != reference {
					t.Errorf("expected %q to be equal to %q, but sorts to %q", input, reference, sorted)
				}
			}
		}
	}

	// but any two elements from different lists may not be identical
	for i, references := range testCases {
		for j, inputs := range testCases {
			if i == j {
				continue
			}
			for _, reference := range references {
				for _, input := range inputs {
					sorted := sortCommaSeparatedLikeInReference(input, reference)
					if sorted == reference {
						t.Errorf("expected %q to be different from %q, but sorts to %q", input, reference, sorted)
					}
				}
			}
		}
	}
}
