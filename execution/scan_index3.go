//  Copyright (c) 2017 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package execution

import (
	"encoding/json"
	"math"

	"github.com/couchbase/query/datastore"
	"github.com/couchbase/query/errors"
	"github.com/couchbase/query/expression"
	"github.com/couchbase/query/plan"
	"github.com/couchbase/query/sort"
	"github.com/couchbase/query/util"
	"github.com/couchbase/query/value"
)

var _INDEXSCAN3_OP_POOL util.FastPool

func init() {
	util.NewFastPool(&_INDEXSCAN3_OP_POOL, func() interface{} {
		return &IndexScan3{}
	})
}

type IndexScan3 struct {
	base
	conn     *datastore.IndexConnection
	plan     *plan.IndexScan3
	children []Operator
}

func NewIndexScan3(plan *plan.IndexScan3, context *Context) *IndexScan3 {
	rv := _INDEXSCAN3_OP_POOL.Get().(*IndexScan3)
	rv.plan = plan

	newBase(&rv.base, context)
	rv.output = rv
	return rv
}

func (this *IndexScan3) Accept(visitor Visitor) (interface{}, error) {
	return visitor.VisitIndexScan3(this)
}

func (this *IndexScan3) Copy() Operator {
	rv := _INDEXSCAN3_OP_POOL.Get().(*IndexScan3)
	rv.plan = this.plan
	this.base.copy(&rv.base)
	return rv
}

func (this *IndexScan3) RunOnce(context *Context, parent value.Value) {
	this.once.Do(func() {
		defer context.Recover(&this.base) // Recover from any panic
		if !this.active() {
			return
		}
		defer this.close(context)
		this.switchPhase(_EXECTIME)
		this.setExecPhase(INDEX_SCAN, context)
		defer func() { this.switchPhase(_NOTIME) }() // accrue current phase's time
		defer this.notify()                          // Notify that I have stopped

		this.conn = datastore.NewIndexConnection(context)
		defer this.conn.Dispose()  // Dispose of the connection
		defer this.conn.SendStop() // Notify index that I have stopped

		go this.scan(context, this.conn, parent)

		ok := true
		var docs uint64 = 0

		var countDocs = func() {
			if docs > 0 {
				context.AddPhaseCount(INDEX_SCAN, docs)
			}
		}
		defer countDocs()

		// for right hand side of nested-loop join we don't want to include parent values
		// in the returned scope value
		scope_value := parent
		covers := this.plan.Covers()
		lcovers := len(covers)

		var entryKeys []int
		proj := this.plan.Projection()
		if proj != nil {
			entryKeys = proj.EntryKeys
		}

		if this.plan.Term().IsUnderNL() {
			scope_value = nil
		}

		for ok {
			entry, cont := this.getItemEntry(this.conn)
			if cont {
				if entry != nil {
					av := this.newEmptyDocumentWithKey(entry.PrimaryKey, scope_value, context)
					covers := this.plan.Covers()
					if lcovers > 0 {

						for c, v := range this.plan.FilterCovers() {
							av.SetCover(c.Text(), v)
						}

						// Matches planner.builder.buildCoveringScan()
						for i, ek := range entry.EntryKey {
							if proj == nil || i < len(entryKeys) {
								if i < len(entryKeys) {
									i = entryKeys[i]
								}

								if i < lcovers {
									av.SetCover(covers[i].Text(), ek)
								}
							}
						}

						// Matches planner.builder.buildCoveringScan()
						if proj == nil || proj.PrimaryKey {
							av.SetCover(covers[len(covers)-1].Text(),
								value.NewValue(entry.PrimaryKey))
						}

						av.SetField(this.plan.Term().Alias(), av)
					}

					av.SetBit(this.bit)
					ok = this.sendItem(av)
					docs++
					if docs > _PHASE_UPDATE_COUNT {
						context.AddPhaseCount(INDEX_SCAN, docs)
						docs = 0
					}
				} else {
					ok = false
				}
			} else {
				return
			}
		}
	})
}

func (this *IndexScan3) scan(context *Context, conn *datastore.IndexConnection, parent value.Value) {
	defer context.Recover(nil) // Recover from any panic

	plan := this.plan

	// for nested-loop join we need to pass in values from left-hand-side (outer) of the join
	// for span evaluation

	groupAggs := plan.GroupAggs()
	dspans, empty, err := evalSpan3(plan.Spans(), parent, plan.HasDynamicInSpan(), context)

	// empty span with Index aggregation is present and no group by requies produce default row.
	// Therefore, do IndexScan

	if err != nil || (empty && (groupAggs == nil || len(groupAggs.Group) > 0)) {
		if err != nil {
			context.Error(errors.NewEvaluationError(err, "span"))
		}
		conn.Sender().Close()
		return
	}

	offset := evalLimitOffset(this.plan.Offset(), nil, int64(0), this.plan.Covering(), context)
	limit := evalLimitOffset(this.plan.Limit(), nil, math.MaxInt64, this.plan.Covering(), context)
	scanVector := context.ScanVectorSource().ScanVector(plan.Term().Namespace(), plan.Term().Keyspace())

	indexProjection, indexOrder, indexGroupAggs := planToScanMapping(plan.Index(), plan.Projection(),
		plan.OrderTerms(), plan.GroupAggs(), plan.Covers())

	plan.Index().Scan3(context.RequestId(), dspans, plan.Reverse(), plan.Distinct(),
		indexProjection, offset, limit, indexGroupAggs, indexOrder,
		context.ScanConsistency(), scanVector, conn)
}

func evalSpan3(pspans plan.Spans2, parent value.Value, hasDynamicInSpan bool, context *Context) (
	datastore.Spans2, bool, error) {
	spans := pspans
	if hasDynamicInSpan {
		numspans := len(pspans)
		minPos := 0
		maxPos := 0
		for _, ps := range pspans {
			for i, rg := range ps.Ranges {
				if !rg.IsDynamicIn() {
					continue
				}

				av, empty, err := evalOne(rg.GetDynamicInExpr(), context, parent)
				if err != nil {
					return nil, false, err
				}
				if !empty && av.Type() == value.ARRAY {
					arr := av.ActualForIndex().([]interface{})
					set := value.NewSet(len(arr), true, false)
					set.AddAll(arr)
					arr = set.Actuals()
					sort.Sort(value.NewSorter(value.NewValue(arr)))
					newlength := numspans + (maxPos-minPos+1)*(len(arr)-1)
					if newlength <= _FULL_SPAN_FANOUT {
						ospans := spans
						spans = make(plan.Spans2, 0, newlength)
						add := 0
						for j, sp := range ospans {
							if j >= minPos && j <= maxPos {
								for _, v := range arr {
									spn := sp.Copy()
									nrg := spn.Ranges[i]
									nrg.Low = expression.NewConstant(v)
									nrg.High = nrg.Low
									nrg.Inclusion = datastore.BOTH
									spans = append(spans, spn)
								}
								add = add + len(arr) - 1
							} else {
								spans = append(spans, sp)
							}
						}
						numspans = len(spans)
						maxPos = maxPos + add
					}
				}
			}
			minPos = maxPos + 1
			maxPos = minPos
		}
	}

	return evalSpan2(spans, parent, context)
}

func planToScanMapping(index datastore.Index, proj *plan.IndexProjection, indexOrderTerms plan.IndexKeyOrders,
	groupAggs *plan.IndexGroupAggregates, covers expression.Covers) (indexProjection *datastore.IndexProjection,
	indexOrder datastore.IndexKeyOrders, indexGroupAggs *datastore.IndexGroupAggregates) {

	if proj != nil {
		indexProjection = &datastore.IndexProjection{EntryKeys: proj.EntryKeys, PrimaryKey: proj.PrimaryKey}
	}

	if len(indexOrderTerms) > 0 {
		indexOrder = make(datastore.IndexKeyOrders, 0, len(indexOrderTerms))
		for _, o := range indexOrderTerms {
			indexOrder = append(indexOrder, &datastore.IndexKeyOrder{KeyPos: o.KeyPos, Desc: o.Desc})
		}
	}

	if groupAggs != nil {
		var group datastore.IndexGroupKeys
		var aggs datastore.IndexAggregates

		if len(groupAggs.Group) > 0 {
			group = make(datastore.IndexGroupKeys, 0, len(groupAggs.Group))
			for _, g := range groupAggs.Group {
				group = append(group, &datastore.IndexGroupKey{EntryKeyId: g.EntryKeyId,
					KeyPos: g.KeyPos, Expr: g.Expr})
			}
		}

		if len(groupAggs.Aggregates) > 0 {
			aggs = make(datastore.IndexAggregates, 0, len(groupAggs.Aggregates))
			for _, a := range groupAggs.Aggregates {
				aggs = append(aggs, &datastore.IndexAggregate{Operation: a.Operation,
					EntryKeyId: a.EntryKeyId, KeyPos: a.KeyPos, Expr: a.Expr,
					Distinct: a.Distinct})
			}
		}

		// include META().id which is at nKeys+1
		nKeys := len(index.RangeKey())
		IndexKeyNames := make([]string, 0, nKeys+1)
		for i := 0; i <= nKeys; i++ {
			IndexKeyNames = append(IndexKeyNames, covers[i].Text())
		}

		indexGroupAggs = &datastore.IndexGroupAggregates{Name: groupAggs.Name, Group: group,
			Aggregates: aggs, DependsOnIndexKeys: groupAggs.DependsOnIndexKeys,
			IndexKeyNames: IndexKeyNames, OneForPrimaryKey: groupAggs.DistinctDocid,
			AllowPartialAggr: groupAggs.Partial}
	}

	return
}

func (this *IndexScan3) MarshalJSON() ([]byte, error) {
	r := this.plan.MarshalBase(func(r map[string]interface{}) {
		this.marshalTimes(r)
	})
	return json.Marshal(r)
}

// send a stop
func (this *IndexScan3) SendStop() {
	this.connSendStop(this.conn)
}

func (this *IndexScan3) Done() {
	this.baseDone()
	this.conn = nil
	if this.isComplete() {
		_INDEXSCAN3_OP_POOL.Put(this)
	}
}

const _FULL_SPAN_FANOUT = 8192
