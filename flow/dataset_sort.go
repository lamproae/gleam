package flow

import (
	"fmt"
	"io"

	"github.com/chrislusf/gleam/util"
	"github.com/psilva261/timsort"
	"github.com/ugorji/go/codec"
)

var (
	msgpackHandler codec.MsgpackHandle
)

type pair struct {
	keys []interface{}
	data []byte
}

func (d *Dataset) Sort(indexes ...int) *Dataset {
	if len(indexes) == 0 {
		indexes = []int{1}
	}
	orderBys := getOrderBysFromIndexes(indexes)
	ret := d.LocalSort(orderBys)
	if len(d.Shards) > 0 {
		ret = ret.MergeSortedTo(1, orderBys)
	}
	return ret
}

func (d *Dataset) SortBy(orderBys ...OrderBy) *Dataset {
	if len(orderBys) == 0 {
		orderBys = []OrderBy{OrderBy{1, Ascending}}
	}
	ret := d.LocalSort(orderBys)
	if len(d.Shards) > 0 {
		ret = ret.MergeSortedTo(1, orderBys)
	}
	return ret
}

func (d *Dataset) LocalSort(orderBys []OrderBy) *Dataset {
	if isOrderByEquals(d.IsLocalSorted, orderBys) {
		return d
	}

	ret, step := add1ShardTo1Step(d)
	ret.IsLocalSorted = orderBys
	step.Name = "LocalSort"
	step.Params["orderBys"] = orderBys
	step.FunctionType = TypeLocalSort
	step.Function = func(task *Task) {
		outChan := task.OutputShards[0].IncomingChan

		LocalSort(task.InputChans[0].Reader, outChan.Writer, orderBys)

		for _, shard := range task.OutputShards {
			shard.IncomingChan.Writer.Close()
		}
	}
	return ret
}

func (d *Dataset) MergeSortedTo(partitionCount int, orderBys []OrderBy) (ret *Dataset) {
	if len(d.Shards) == partitionCount {
		return d
	}
	ret = d.FlowContext.newNextDataset(partitionCount)
	everyN := len(d.Shards) / partitionCount
	if len(d.Shards)%partitionCount > 0 {
		everyN++
	}
	step := d.FlowContext.AddLinkedNToOneStep(d, everyN, ret)
	step.Name = fmt.Sprintf("MergeSortedTo %d", partitionCount)
	step.Params["orderBys"] = orderBys
	step.FunctionType = TypeMergeSortedTo
	step.Function = func(task *Task) {
		outChan := task.OutputShards[0].IncomingChan

		var inChans []io.Reader
		for _, pipe := range task.InputChans {
			inChans = append(inChans, pipe.Reader)
		}

		MergeSortedTo(inChans, outChan.Writer, orderBys)

		for _, shard := range task.OutputShards {
			shard.IncomingChan.Writer.Close()
		}

	}
	return ret
}

func LocalSort(inChan io.Reader, outChan io.Writer, orderBys []OrderBy) {
	var kvs []interface{}
	indexes := getIndexesFromOrderBys(orderBys)
	err := util.ProcessMessage(inChan, func(input []byte) error {
		if keys, err := util.DecodeRowKeys(input, indexes); err != nil {
			return fmt.Errorf("%v: %+v", err, input)
		} else {
			kvs = append(kvs, pair{keys: keys, data: input})
		}
		return nil
	})
	if err != nil {
		fmt.Printf("Sort>Failed to read input data:%v\n", err)
	}
	if len(kvs) == 0 {
		return
	}
	timsort.Sort(kvs, func(a, b interface{}) bool {
		x, y := a.(pair), b.(pair)
		for i, order := range orderBys {
			if order.Order > 0 {
				if util.LessThan(x.keys[i], y.keys[i]) {
					return true
				}
			} else {
				if !util.LessThan(x.keys[i], y.keys[i]) {
					return true
				}
			}
		}
		return false
	})

	for _, kv := range kvs {
		// println("sorted key", string(kv.(pair).keys[0].([]byte)))
		util.WriteMessage(outChan, kv.(pair).data)
	}
}

func MergeSortedTo(inChans []io.Reader, outChan io.Writer, orderBys []OrderBy) {
	indexes := getIndexesFromOrderBys(orderBys)
	pq := util.NewPriorityQueue(func(a, b interface{}) bool {
		x, y := a.([]byte), b.([]byte)
		xKeys, _ := util.DecodeRowKeys(x, indexes)
		yKeys, _ := util.DecodeRowKeys(y, indexes)
		for i, order := range orderBys {
			if order.Order > 0 {
				if util.LessThan(xKeys[i], yKeys[i]) {
					return true
				}
			} else {
				if !util.LessThan(xKeys[i], yKeys[i]) {
					return true
				}
			}
		}
		return false
	})
	// enqueue one item to the pq from each channel
	for shardId, shardChan := range inChans {
		if x, err := util.ReadMessage(shardChan); err == nil {
			pq.Enqueue(x, shardId)
		}
	}
	for pq.Len() > 0 {
		t, shardId := pq.Dequeue()
		util.WriteMessage(outChan, t.([]byte))
		if x, err := util.ReadMessage(inChans[shardId]); err == nil {
			pq.Enqueue(x, shardId)
		}
	}
}

func isOrderByEquals(a []OrderBy, b []OrderBy) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v.Index != b[i].Index || v.Order != b[i].Order {
			return false
		}
	}
	return true
}

func getIndexesFromOrderBys(orderBys []OrderBy) (indexes []int) {
	for _, o := range orderBys {
		indexes = append(indexes, o.Index)
	}
	return
}

func getOrderBysFromIndexes(indexes []int) (orderBys []OrderBy) {
	for _, i := range indexes {
		orderBys = append(orderBys, OrderBy{i, Ascending})
	}
	return
}
