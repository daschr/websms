package main

import (
	"log"
	"os/exec"
	"bytes"
	"io"
	"os"
	"encoding/json"
	"regexp"
	"strings"
	"errors"
	"fmt"
	"net/http"
	"time"
	"sort"
)
/*
const (
	NUM="nummer"
	LIST="liste"
	TEXT="text"
	DATE="datum"
	SENDER="absender"
	APIKEY="key"
	//Mon Jan 2 15:04:05 MST 2006
	TIMEFORMAT="20060102-1504"
	NTIMEFORMAT="200601021504"
	TIMEZONE="Europe/Berlin"

	SENDSMS="/usr/local/bin/sendsms"
)
*/

func con(m map[string][]string, s string) bool{
	for x,_:=range m{if x == s {return true}}
	return false
}

func wr_resp(c http.ResponseWriter, status int ,text string){
	c.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.WriteHeader(status)
	io.WriteString(c,text)
}

type SMSRequest struct{
	numbers []string
	text string
	date string
	sender string
}

func (sms SMSRequest) send(config *Config) (error){
	for _,x:= range sms.numbers{
		cmd:=exec.Command((*config).SENDSMS,x,sms.text)
		var buf bytes.Buffer
		cmd.Stdout=&buf
		e:=cmd.Run()
		if e != nil{
			//log.Printf("[ERROR] SENT %s \"%s\" => \"%s\" Error: \"%s\"",x,sms.text,buf.String(),e)
			(*config).LOG(true,fmt.Sprintf("[ERROR] SENT %s \"%s\" => \"%s\" Error: \"%s\"",x,sms.text,buf.String(),e))
			return e
		}
		//log.Printf("SENT %s \"%s\" => \"%s\"",x,sms.text,buf.String())
		if config.Verbose {
			(*config).LOG(false,fmt.Sprintf("SENT %s \"%s\" => \"%s\"",x,sms.text,buf.String()))
		}else{
			(*config).LOG(false,fmt.Sprintf("SENT %s",x))
		}
	}
	return nil
}

func get_sms(config *Config,m map[string][]string) (SMSRequest,error){
	req:=SMSRequest{}
	if con(m,(*config).LIST) && len(m[(*config).LIST][0]) != 0 {
		req.numbers=strings.Split(m[(*config).LIST][0],";")
	}else if con(m,(*config).NUM) && len(m[(*config).NUM][0])!=0{
		req.numbers=append(req.numbers,m[(*config).NUM][0])
	}else{
		return req,errors.New("ERROR: No number...")
	}
	if ! con(m,(*config).TEXT) || len(m[(*config).TEXT][0]) ==0{
		return req,errors.New("ERROR: No Text...")
	}else {
		req.text=m[(*config).TEXT][0]
	}
	if con(m,(*config).DATE){
		if ztime,e:=time.Parse((*config).TIMEFORMAT,m[(*config).DATE][0]); e ==nil {
			req.date=ztime.Format((*config).TIMEFORMAT)
		}else{
			//log.Println(e)
			return req,errors.New("ERROR: Invalid Date...")
		}
	}
	if con(m,(*config).SENDER){ req.sender=m[(*config).SENDER][0] }
	return req, nil
}

type SMSQueue struct{
	queue map[string][]SMSRequest
	sorted_stamps []string
}

func QueueWatcher(config *Config,smsq *SMSQueue) {
		germ,_:=time.LoadLocation((*config).TIMEZONE)
		for{
			for stamppos,tstamp:=range (*smsq).sorted_stamps{
				if reqt,e:=time.ParseInLocation((*config).TIMEFORMAT,tstamp,germ); e == nil && time.Now().After(reqt){
					for _,sms:= range (*smsq).queue[tstamp]{_=sms.send(config)}
					delete((*smsq).queue,tstamp)
					(*smsq).sorted_stamps=append((*smsq).sorted_stamps[:stamppos],(*smsq).sorted_stamps[stamppos+1:]...)
				}else{break}
			}
			time.Sleep(100*time.Millisecond)
		}
		//log.Println("QueueWatcher stopped...")
		(*config).LOG(true,"QueueWatcher stopped...")
}

func add(smsq *SMSQueue,sms SMSRequest){
	if _,ok:=(*smsq).queue[sms.date]; ok{
		(*smsq).queue[sms.date]=append((*smsq).queue[sms.date],sms)
	}else{
		new_stamps:=append((*smsq).sorted_stamps,sms.date)
		sort.Strings(new_stamps)
		(*smsq).sorted_stamps=new_stamps
		(*smsq).queue[sms.date]=[]SMSRequest{sms}
	}
}

func sms_api(config *Config, smsqueue *SMSQueue,c http.ResponseWriter, r *http.Request){
	query:=r.URL.Query()
	lex,_:=regexp.Compile(fmt.Sprintf("%s=[^&]*",(*config).LIST))
	if subs:=lex.FindString(r.URL.RawQuery); len(subs) !=0{
		query[(*config).LIST][0]=strings.Split(subs,"=")[1]
	}
	if !(con(query,(*config).APIKEY) && query[(*config).APIKEY][0] == (*config).Apikey){
		wr_resp(c,400,fmt.Sprint("Wrong ",(*config).APIKEY))
		(*config).LOG(true,fmt.Sprint(r.URL," => Wrong ",(*config).APIKEY))
		return
	}
	sms,e:=get_sms(config,query)
	if e != nil{
		wr_resp(c,400,e.Error())
		(*config).LOG(true,fmt.Sprint(r.URL," => ",e.Error()))
	}else{
		if len(sms.date)==0{
			e= sms.send(config)
			if e!=nil{
				wr_resp(c,400,e.Error())
				(*config).LOG(true,e.Error())
			}else{  wr_resp(c,200,"SMS sent") }
		}else{
			add(smsqueue,sms)
			wr_resp(c,200,"SMS queued")
		}
	}
}

type Config struct{
	Port int
	Addr string
	Apikey string
	Verbose bool
	NUM string //="nummer"
        LIST string //="liste"
        TEXT string //="text"
        DATE string //="datum"
        SENDER string //="absender"
        APIKEY  string //="key"
        //Mon Jan 2 15:04:05 MST 2006
        TIMEFORMAT string //="20060102-1504"
        TIMEZONE string //="Europe/Berlin"

        SENDSMS string //="/usr/local/bin/sendsms"
	LOGCMD string //="syslog2db"
	ERRPREFIX string
	LOGTIMEFORMAT string
}
func (c Config) LOG(is_error bool,msg string){
	form_msg:=func() (string){
						if is_error{return fmt.Sprintf("%s[websms] %s: %s",c.ERRPREFIX,time.Now().Format(c.LOGTIMEFORMAT),msg)
						}else{return fmt.Sprintf("[websms] %s: %s",time.Now().Format(c.LOGTIMEFORMAT),msg)}
				}()
	fmt.Println(form_msg)
	cmd:=exec.Command(c.LOGCMD,form_msg)
	e:= cmd.Run()
	if e != nil{
		log.Println("Error running ERRCMD: ",e.Error())
	}
}

func parseConfig() (Config){
	if len(os.Args) != 2 { log.Fatal("No config given!") }
	of,e:=os.Open(os.Args[1])
	defer of.Close()
	if e != nil{log.Fatal(e)}
	dec:=json.NewDecoder(of)
	conf:=Config{}
	e=dec.Decode(&conf)
	if e != nil{log.Fatal(e)}
	return conf
}

func main(){
	conf:=parseConfig()
	smsqueue:=SMSQueue{}
	smsqueue.queue=make(map[string][]SMSRequest)
	smsqueue.sorted_stamps=[]string{}
	go QueueWatcher(&conf,&smsqueue)
	http.HandleFunc("/send_sms",func(c http.ResponseWriter, r *http.Request){
		sms_api(&conf,&smsqueue,c,r)
	})
	http.ListenAndServe(fmt.Sprintf("%s:%d",conf.Addr,conf.Port),nil)
}
