//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package algebra

import (
	"github.com/couchbaselabs/query/expression"
	"github.com/couchbaselabs/query/value"
)

type Max struct {
	aggregateBase
}

func NewMax(argument expression.Expression) Aggregate {
	return &Max{aggregateBase{argument: argument}}
}

func (this *Max) Evaluate(item value.Value, context expression.Context) (result value.Value, e error) {
	return this.evaluate(this, item, context)
}

func (this *Max) Fold() (expression.Expression, error) {
	return this.fold(this)
}

func (this *Max) Formalize(forbidden, allowed value.Value, keyspace string) (expression.Expression, error) {
	return this.formalize(this, forbidden, allowed, keyspace)
}

func (this *Max) VisitChildren(visitor expression.Visitor) (expression.Expression, error) {
	return this.visitChildren(this, visitor)
}

func (this *Max) Constructor() expression.FunctionConstructor {
	return func(arguments expression.Expressions) expression.Function {
		return NewMax(arguments[0])
	}
}

func (this *Max) Default() value.Value {
	return value.NULL_VALUE
}

func (this *Max) CumulateInitial(item, cumulative value.Value, context Context) (value.Value, error) {
	item, e := this.argument.Evaluate(item, context)
	if e != nil {
		return nil, e
	}

	if item.Type() <= value.NULL {
		return cumulative, nil
	}

	return this.cumulatePart(item, cumulative, context)
}

func (this *Max) CumulateIntermediate(part, cumulative value.Value, context Context) (value.Value, error) {
	return this.cumulatePart(part, cumulative, context)
}

func (this *Max) ComputeFinal(cumulative value.Value, context Context) (value.Value, error) {
	return cumulative, nil
}

func (this *Max) cumulatePart(part, cumulative value.Value, context Context) (value.Value, error) {
	if part == value.NULL_VALUE {
		return cumulative, nil
	} else if cumulative == value.NULL_VALUE {
		return part, nil
	} else if part.Collate(cumulative) > 0 {
		return part, nil
	} else {
		return cumulative, nil
	}
}
