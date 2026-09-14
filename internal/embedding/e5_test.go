package embedding

import (
 "context"
 "encoding/json"
 "errors"
 "math"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
)

func TestE5MultilingualQueries(t *testing.T) {
 ctx := context.Background()
 e := E5{}
 passages := []string{
  "Employees receive twenty days of paid vacation each year.",
  "The database backup runs every night and is kept for thirty days.",
  "La visita dal dentista è fissata per martedì mattina.",
  "Vlak do Prahy odjíždí v osm hodin ráno.",
 }
 docs,err := e.Embed(ctx,passages); if err != nil { t.Fatal(err) }
 cases := []struct{q string; want int}{
  {"How much time off do staff receive?",0},
  {"Quanti giorni di ferie spettano ai dipendenti?",0},
  {"Kolik dní dovolené mají zaměstnanci?",0},
  {"Wann wird die Datenbank gesichert?",1},
  {"When is the dental appointment?",2},
  {"A che ora parte il treno per Praga?",3},
  {"数据库备份保留多久？",1},
 }
 for _,tc := range cases {
  t.Run(tc.q,func(t *testing.T){
   q,err := e.EmbedQuery(ctx,[]string{tc.q}); if err != nil { t.Fatal(err) }
   best := -1; score := -2.0
   for i,d := range docs { s,err := Cosine(q[0],d); if err != nil { t.Fatal(err) }; if s > score { best,score = i,s } }
   if best != tc.want { t.Fatalf("best passage %d, want %d",best,tc.want) }
   var norm float64
   for _,v := range q[0] { norm += float64(v)*float64(v) }
   if math.Abs(norm-1) > 1e-5 { t.Fatalf("norm = %g",norm) }
  })
 }
}

func TestTokenWindowsRetainTailAndPrefix(t *testing.T) {
 ids := []int64{123,456}
 for i:=0;i<1500;i++ { ids=append(ids,int64(i+1000)) }
 windows := tokenWindows(ids,2)
 var body []int64
 for _,w := range windows {
  if len(w)>512 || w[0]!=0 || w[1]!=123 || w[2]!=456 || w[len(w)-1]!=2 { t.Fatalf("invalid window: %v",w) }
  body=append(body,w[3:len(w)-1]...)
 }
 if len(body)!=1500 { t.Fatalf("lost tokens: %d",len(body)) }
 for i,v:=range body { if v!=int64(i+1000) { t.Fatalf("token %d = %d",i,v) } }
}

func TestE5LongTextIncludesTail(t *testing.T) {
 e:=E5{}; ctx:=context.Background()
 prefix:=strings.Repeat("The office opens each morning. ",180)
 a,err:=e.Embed(ctx,[]string{prefix, prefix+strings.Repeat("The volcano erupted and covered the island with lava. ",100)})
 if err!=nil {t.Fatal(err)}
 similarity,err:=Cosine(a[0],a[1]); if err!=nil {t.Fatal(err)}
 if similarity > .999 {t.Fatalf("long tail was ignored: similarity %g",similarity)}
}

func TestE5EmptyBatchAndCancellation(t *testing.T) {
 e:=E5{}
 v,err:=e.Embed(context.Background(),nil); if err!=nil||v!=nil {t.Fatalf("%v %v",v,err)}
 v,err=e.Embed(context.Background(),[]string{}); if err!=nil||len(v)!=0 {t.Fatalf("%v %v",v,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=e.EmbedQuery(ctx,[]string{"hello"});!errors.Is(err,context.Canceled){t.Fatalf("got %v",err)}
}

func TestE5ConcurrentCalls(t *testing.T) {
 var wg sync.WaitGroup
 for i:=0;i<4;i++ {wg.Add(1);go func(){defer wg.Done();if _,err:=(E5{}).EmbedQuery(context.Background(),[]string{"Find the train timetable"});err!=nil{t.Error(err)}}()}
 wg.Wait()
}

func TestAssetValidationRejectsMissingAndCorruptFiles(t *testing.T) {
 dir:=t.TempDir()
 if err:=verifyAssets(dir);err==nil {t.Fatal("accepted missing manifest")}
 m:=assetManifest{ModelID:(E5{}).ID(),Files:map[string]string{"model.onnx":strings.Repeat("0",64)}}
 raw,_:=json.Marshal(m)
 if err:=os.WriteFile(filepath.Join(dir,"manifest.json"),raw,0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"model.onnx"),[]byte("corrupt"),0600);err!=nil{t.Fatal(err)}
 if err:=verifyAssets(dir);err==nil||!strings.Contains(err.Error(),"checksum"){t.Fatalf("got %v",err)}
}

func BenchmarkE5Query(b *testing.B) {
 e:=E5{};ctx:=context.Background()
 if _,err:=e.EmbedQuery(ctx,[]string{"warmup"});err!=nil{b.Fatal(err)}
 b.ResetTimer()
 for i:=0;i<b.N;i++{if _,err:=e.EmbedQuery(ctx,[]string{"Quanti giorni di ferie spettano ai dipendenti?"});err!=nil{b.Fatal(err)}}
}

func TestE5MatchesIndependentReference(t *testing.T) {
 path:=os.Getenv("CHIEDI_E5_REFERENCE")
 if path=="" {t.Skip("CI supplies the independent Python ONNX/tokenizer reference")}
 raw,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
 var cases []struct{Kind string;Text string;Vector []float32}
 if err=json.Unmarshal(raw,&cases);err!=nil{t.Fatal(err)}
 if len(cases)<5{t.Fatal("incomplete independent reference")}
 for _,tc:=range cases{
  var got [][]float32
  if tc.Kind=="query"{got,err=(E5{}).EmbedQuery(context.Background(),[]string{tc.Text})}else{got,err=(E5{}).Embed(context.Background(),[]string{tc.Text})}
  if err!=nil{t.Fatal(err)}
  if len(tc.Vector)!=dimensions{t.Fatal("invalid reference dimensions")}
  for i,want:=range tc.Vector{if math.Abs(float64(got[0][i]-want))>2e-4{t.Fatalf("%s coordinate %d: got %g want %g",tc.Text,i,got[0][i],want)}}
 }
}
