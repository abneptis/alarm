package main

import (
  "bytes"
  "io"
  "flag"
  "fmt"
  "os"
  "path"
  "strings"
  "strconv"
  "syscall"
  "time"
)

const (
  die_OK = iota
  die_USAGE
  die_EXEC
  die_NULL
  die_BADTIMESPEC
  die_FDSETUP
  die_WAITERROR
  die_FILEERR
)

const (
  DEFAULT_SIGNAL = 15
  DEFAULT_SIGNAL_STRING = "15"
  SECOND_IN_NS = 1000000000
)


var flag_close_stdin *bool = flag.Bool("close-stdin",false,"Close stdin")
var flag_timeout *string   = flag.String("time","","List of timeouts 'secs[:signal][,...]; default signal is 15, 0 just prints")
var flag_arg0 *string       = flag.String("arg0","","arg0 to use (default=CMD)")
var flag_cwd *string        = flag.String("cwd",os.Getenv("PWD"),"Working directory (default=CWD)")
var flag_verbose *bool      = flag.Bool("verbose",false, "Verbose logging")
var flag_force_timer *bool  = flag.Bool("force-short-timers",false, "Don't auto-adjust values < 1 second")

var flag_log_stdout *string = flag.String("stdout","", "File to log stdout to")
var flag_log_stderr *string = flag.String("stderr","", "File to log stderr to")

var flag_buff_size  *int    = flag.Int("mem", 8, "Amount of memory to use for 'memory' outputs (KB)")

var flag_dump_log    *string = flag.String("dump","onerror", "Dump the logs when (always, onerror, onsuccess)")

var flag_squelch     *bool  = flag.Bool("squelch",false, "Implies -stdout=memory -stderr=memory -dump=onerror")

// TODO: This leaks descriptors on an error;
// since our usage dies on an error, we don't really care.

func memFD(uw io.Writer)(fd *os.File, c io.Closer, err os.Error){
  r, w, err := os.Pipe()
  if err == nil {
    c = w
    fd = w
    go io.Copy(uw, r)
  }
  return
}

func procFileDescriptors(memfd *os.File, proc_done <-chan  int)(fdset []int, icloser func()(err os.Error), err os.Error){
  opened := []io.Closer{}
  icloser = func()(err os.Error){
    for i := range(opened){
      err2 := opened[i].Close()
      if err == nil { err = err2}
    }
    return
  }
  // STDIN
  if *flag_close_stdin {
    fp, err := os.Open("/dev/null")
    if err != nil {
      fmt.Printf("FATAL: Couldn't open /dev/null (required to close stdin)")
      os.Exit(die_NULL)
    }
    fdset = append(fdset, fp.Fd())
    opened = append(opened, fp)
  } else {
    fdset = append(fdset, os.Stdin.Fd())
  }

  // STDOUT
  if *flag_log_stdout == "" {
    fdset = append(fdset, os.Stdout.Fd()) 
  } else {
    var f *os.File
    if *flag_log_stdout == "memory" {
      f = memfd
    } else {
      f, err = os.OpenFile(*flag_log_stdout, os.O_CREATE|os.O_WRONLY, 0666)
    }
    if err == nil {
      fdset = append(fdset, f.Fd())
      opened = append(opened, f)
    }
    if *flag_log_stderr == *flag_log_stdout {
      fdset = append(fdset, f.Fd())
      opened = append(opened, f)
    }
  }

  // STDERR

  if err == nil && *flag_log_stderr != *flag_log_stdout {
    if *flag_log_stderr == "" {
      fdset = append(fdset, os.Stderr.Fd())
    } else { 
      var f *os.File
      if *flag_log_stderr == "memory" {
        f = memfd
      } else {
        f, err = os.OpenFile(*flag_log_stderr, os.O_CREATE|os.O_WRONLY, 0666)
      }
      if err == nil { 
        fdset = append(fdset, f.Fd()) 
        opened = append(opened, f)
      }
    }
  }
  return
}

func Which(cmd string, P []string)(out string){
  dir, file := path.Split(cmd)
  if dir == "" {
    for pe := range(P){
      if fi, err := os.Stat(path.Join(P[pe],dir,file)); err == nil && fi.Mode & 0x0111 != 0 {
        out = path.Join(P[pe],dir,file)
        break
      }
    }
  } else {
    out = cmd
  }
  return
}

type Timer struct {
  Ticks int64
  Pid int
  Signal int
}

func NewTimer(ticks int64, pid, signal int)(*Timer){
  return &Timer{Ticks:ticks, Pid:pid, Signal:signal}
}

func NewTimerString(s string)(T *Timer, err os.Error){
  timer := strings.Split(s,":",2)
  if *flag_verbose {
    fmt.Printf("Parsing timer: '%s'\n", s)
  }
  if len(timer) == 1 {
    timer = append(timer, DEFAULT_SIGNAL_STRING)
  }
  var tval float64
  var signal int
  tval, err = strconv.Atof64(timer[0])
  if err == nil {
    signal, err = strconv.Atoi(timer[1])
  }
  if err == nil {
    if tval < SECOND_IN_NS && ! *flag_force_timer {
      tval *= SECOND_IN_NS
    }
    T = NewTimer(int64(tval),0,signal)
  }
  return
}

// used only for printing, but
// probably has some undisovered issue...
func (self Timer)tickSecs()(float64){
  return float64(self.Ticks)/SECOND_IN_NS
}

func (self Timer)Start(verbose bool)(err os.Error){
  if verbose { fmt.Printf("Pid %d : waiting for %f seconds to send %d\n", self.Pid, self.tickSecs(), self.Signal) }
  time.Sleep(self.Ticks)
  eno := syscall.Kill(self.Pid, self.Signal)
  if self.Signal > 0 && eno != 0 {
    err = os.NewError(syscall.Errstr(eno))
    if verbose { fmt.Printf("Pid %d : Couldn't signal: %v\n", self.Pid, err) }
  } else if self.Signal == 0 && eno == 0 {
    err = os.NewError(fmt.Sprintf("alive after %f seconds\n", self.tickSecs()))
    fmt.Printf("Pid %d : %s", self.Pid, err.String())
  }
  return
}

func main(){
  flag.Parse()
  if flag.NArg() < 1 {
    fmt.Printf("USAGE: alarm [--alarmopts] CMD cmdflags\n")
    os.Exit(die_USAGE)
  }
  if *flag_squelch && *flag_log_stdout == "" { *flag_log_stdout = "memory" }
  if *flag_squelch && *flag_log_stderr == "" { *flag_log_stderr = "memory" }
  args := flag.Args()
  if *flag_arg0 == "" {
    *flag_arg0 = flag.Arg(0)
  } else {
    args = args[1:]
  }
  //  We use a buffered channel so that we don't have to check intent to signal end of proc.
  iodone := make(chan int,1)
  var memfd *os.File
  var memcloser io.Closer
  var err os.Error
  var membuffer *bytes.Buffer
  if *flag_log_stdout == "memory" || *flag_log_stderr == "memory" {
    membuffer = bytes.NewBuffer(make([]byte, 1024 * (*flag_buff_size)))
    memfd, memcloser, err = memFD(membuffer)
    if err != nil {
      fmt.Printf("Couldn't setup memory FD: %v", err)
      os.Exit(die_FDSETUP)
    }
    go func(){
      io.Copy(membuffer, memfd)
    }()
  }
  
  files, close_local, err := procFileDescriptors(memfd, iodone)
  if err != nil {
    fmt.Printf("Couldn't setup file descriptors: %v", err)
    os.Exit(die_FDSETUP)
  }
  arg0 := Which(*flag_arg0, strings.Split(os.Getenv("PATH"),":",-1))
  if arg0 != *flag_arg0 && *flag_verbose {
    fmt.Printf("Implied arg0: (%s) => %s\n", *flag_arg0, arg0)
  }



  
  timers := []*Timer{}
  timer_strings := strings.Split(*flag_timeout,",",-1)
  for ti := range(timer_strings) {
    if timer_strings[ti] == "" {
      continue
    }
    t, err := NewTimerString(timer_strings[ti])
    if err != nil {
      fmt.Printf("Couldn't parse timer string '%s': %v\n", timer_strings[ti], err)
      os.Exit(die_BADTIMESPEC)
    }
    if t.Ticks > 0 {
      timers = append(timers, t) 
    } else {
      if *flag_verbose {
        fmt.Printf("WARN: Ignoring useless timer %s\n", timer_strings[ti])
      }
    }
  }

  pid, eno := syscall.ForkExec(arg0, args, &syscall.ProcAttr {
    Dir: os.Getenv("PWD"),
    Env: os.Environ(),
    Files: files,
  })
  if eno != 0 {
    fmt.Printf("There was an error running the command: %s\n", syscall.Errstr(eno))
    os.Exit(die_EXEC)
  }
  for t := range(timers) {
    timers[t].Pid = pid
    go timers[t].Start(*flag_verbose)
  }
  // The process has started, so we close our dup()'s of the FIFO (if any)
  if memcloser != nil {
    memcloser.Close()
  }
  close_local()

  wmsg, err := os.Wait(pid, 0)
  iodone <- 1

  if err != nil {
    fmt.Printf("There was an error waiting for pid: %v", err)
    os.Exit(die_WAITERROR)
  }
  if *flag_verbose {
    fmt.Printf("PID: %d exited with status %d\n", pid, wmsg.ExitStatus())
  }

  if (wmsg.ExitStatus() != 0 && *flag_dump_log == "onerror" ) || 
     (*flag_dump_log == "always")  &&
     (*flag_log_stdout == "memory" || *flag_log_stderr == "memory" ) {
    if *flag_verbose {
      fmt.Printf("Dumping logs [%d]\n----------\n", wmsg.ExitStatus())
    }
    if membuffer != nil {
      _, err = io.Copy(os.Stdout, membuffer)
    } else {

    }
    if *flag_verbose {
      fmt.Printf("\n----------\nLogs complete: %v", err)
    }
  }
  // We're ok, but we'd like to bubble up the status
  os.Exit(wmsg.ExitStatus())
}
