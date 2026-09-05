// Package source opens document files without waiting on named pipes.
package source

import (
 "fmt"
 "os"
)

func Open(path string)(*os.File,error){
 f,err:=os.OpenFile(path,os.O_RDONLY|nonblock,0);if err!=nil{return nil,err}
 info,err:=f.Stat();if err!=nil{f.Close();return nil,err}
 if !info.Mode().IsRegular(){f.Close();return nil,fmt.Errorf("source is not a regular file: %s",path)}
 return f,nil
}
