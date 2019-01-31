//  Copyright (c) 2019 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package planner

import (
	"github.com/couchbase/query/algebra"
	"github.com/couchbase/query/plan"
)

func (this *builder) VisitCreateFunction(stmt *algebra.CreateFunction) (interface{}, error) {

	// switch on len(stmt.Name().Components) here to handle scoped functions
	stmt.Name().SetComponents([]string{this.namespace})
	return plan.NewCreateFunction(stmt), nil
}

func (this *builder) VisitDropFunction(stmt *algebra.DropFunction) (interface{}, error) {

	// switch on len(stmt.Name().Components) here to handle scoped functions
	stmt.Name().SetComponents([]string{this.namespace})
	return plan.NewDropFunction(stmt), nil
}

func (this *builder) VisitExecuteFunction(stmt *algebra.ExecuteFunction) (interface{}, error) {

	// switch on len(stmt.Name().Components) here to handle scoped functions
	stmt.Name().SetComponents([]string{this.namespace})
	return plan.NewExecuteFunction(stmt), nil
}