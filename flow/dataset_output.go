package flow

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/chrislusf/gleam/util"
)

// Output concurrently collects outputs from previous step to the driver.
func (d *Dataset) Output(f func(io.Reader) error) *Dataset {
	step := d.FlowContext.AddAllToOneStep(d, nil)
	step.IsOnDriverSide = true
	step.Name = "Output"
	step.Function = func(task *Task) {
		var wg sync.WaitGroup
		for _, inChan := range task.InputChans {
			wg.Add(1)
			go func(inChan *util.Piper) {
				defer wg.Done()
				f(inChan.Reader)
				inChan.Reader.Close()
			}(inChan)
		}
		wg.Wait()
	}
	return d
}

// PipeOut writes to writer.
// If previous step is a Pipe() or PipeAsArgs(), the output is written as is.
// Otherwise, each row of output is written in tab-separated lines.
func (d *Dataset) PipeOut(writer io.Writer) *Dataset {
	fn := func(inChan io.Reader) error {
		if d.Step.IsPipe {
			_, err := io.Copy(writer, inChan)
			return err
		}
		return util.FprintRowsFromChannel(inChan, writer, "\t", "\n")
	}
	return d.Output(fn)
}

// Fprintf formats using the format for each row and writes to writer.
func (d *Dataset) Fprintf(writer io.Writer, format string) *Dataset {
	fn := func(inChan io.Reader) error {
		if d.Step.IsPipe {
			return util.TsvPrintf(inChan, writer, format)
		}
		return util.Fprintf(inChan, writer, format)
	}
	return d.Output(fn)
}

// SaveFirstRowTo saves the first row's values into the operands.
func (d *Dataset) SaveFirstRowTo(decodedObjects ...interface{}) *Dataset {
	fn := func(inChan io.Reader) error {
		if d.Step.IsPipe {
			return util.TakeTsv(inChan, 1, func(args []string) error {
				for i, o := range decodedObjects {
					if i >= len(args) {
						break
					}
					if v, ok := o.(*string); ok {
						*v = args[i]
					} else {
						return fmt.Errorf("Should save to *string.")
					}
				}
				return nil
			})
		}

		return util.TakeMessage(inChan, 1, func(encodedBytes []byte) error {
			if err := util.DecodeRowTo(encodedBytes, decodedObjects...); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to decode byte: %v\n", err)
				return err
			}
			return nil
		})
	}
	return d.Output(fn)
}
