// word_count.go
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/chrislusf/gleam"
)

func main() {

	fileNames, err := filepath.Glob("/Users/chris/Downloads/txt/en/ep-08-03-*.txt")
	if err != nil {
		log.Fatal(err)
	}

	gleam.New().Strings(fileNames).Partition(3).PipeAsArgs("cat $1").FlatMap(`
      function(line)
	    log("input:"..line)
        return line:gmatch("%w+")
      end
    `).Map(`
      function(word)
        return word, 1
      end
    `).ReduceByKey(`
      function(x, y)
        return x + y
      end
    `).Map(`
      function(k, v)
        return k .. " " .. v
      end
    `).Pipe("sort -n -k 2").Fprintf(os.Stdout, "%s\n").Run()

}
