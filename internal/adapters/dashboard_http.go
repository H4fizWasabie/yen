package adapters

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

type DashboardHTTP struct {
	Dashboard   Dashboard
	AccessToken string
}

func (h DashboardHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if r.URL.Path == "/" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, dashboardHTML)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if r.URL.Path == "/api/login" {
			h.login(w, r)
			return
		}
		if !h.authorized(w, r) {
			return
		}
	}
	if r.URL.Path == "/api/sessions" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": dashboardSessions(h.Dashboard.Service.Registry, h.Dashboard.Service.Runner)})
		return
	}
	if r.URL.Path == "/api/sessions" && r.Method == http.MethodPost {
		h.newSession(w)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/sessions/") {
		h.session(w, r)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func dashboardSessions(registry *conversation.Registry, runner *runtime.Runner) []map[string]any {
	if registry == nil {
		return []map[string]any{}
	}
	seen := make(map[string]bool)
	sessions := make([]map[string]any, 0)
	for _, link := range registry.List("") {
		if link.Adapter != "dashboard" && link.Adapter != "telegram" || seen[link.ConversationID] {
			continue
		}
		seen[link.ConversationID] = true
		modified, messageCount, title := link.CreatedAt, 0, link.ConversationID
		if runner != nil {
			if opened, err := runner.OpenSession(link); err == nil {
				modified = opened.LastTimestamp()
				messages := opened.Messages()
				messageCount = len(messages)
				title = dashboardSessionTitle(messages, title)
			}
		}
		sessions = append(sessions, map[string]any{
			"id": link.ConversationID, "channel": link.Adapter, "title": title,
			"modified": modified, "messageCount": messageCount, "path": "",
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i]["modified"].(string) > sessions[j]["modified"].(string)
	})
	return sessions
}

func (h DashboardHTTP) authorized(w http.ResponseWriter, r *http.Request) bool {
	if h.AccessToken == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Dashboard access token is not configured"})
		return false
	}
	candidate := bearerToken(r.Header.Get("Authorization"))
	if candidate == "" {
		candidate = cookieToken(r.Header.Get("Cookie"))
	}
	if secureToken(candidate, h.AccessToken) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="Theoses dashboard"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Dashboard authentication required"})
	return false
}

func (h DashboardHTTP) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	if h.AccessToken == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Dashboard access token is not configured"})
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || json.Unmarshal(body, &input) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid login body"})
		return
	}
	if !secureToken(input.Token, h.AccessToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="Theoses dashboard"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid dashboard token"})
		return
	}
	w.Header().Set("Set-Cookie", "theoses_dashboard_token="+url.QueryEscape(h.AccessToken)+"; Path=/; Max-Age=31536000; HttpOnly; SameSite=Strict")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func cookieToken(header string) string {
	for _, item := range strings.Split(header, ";") {
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) == 2 && parts[0] == "theoses_dashboard_token" {
			value, err := url.QueryUnescape(parts[1])
			if err == nil {
				return value
			}
		}
	}
	return ""
}

func secureToken(candidate, expected string) bool {
	if len(candidate) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1
}

func (h DashboardHTTP) newSession(w http.ResponseWriter) {
	link, err := h.Dashboard.NewSession()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

func (h DashboardHTTP) session(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "conversation ID is required"})
		return
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation ID"})
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "branch" && r.Method == http.MethodPost:
		var input struct {
			EntryID string `json:"entryId"`
		}
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if readErr != nil || json.Unmarshal(body, &input) != nil || strings.TrimSpace(input.EntryID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "entryId is required"})
			return
		}
		link, found := findDashboardConversation(h.Dashboard.Service.Registry, id)
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		opened, openErr := h.Dashboard.Service.Runner.OpenSession(link)
		if openErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": openErr.Error()})
			return
		}
		if branchErr := opened.Branch(input.EntryID); branchErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": branchErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"leafId": opened.LeafID(), "tree": opened.Tree(), "history": dashboardHistory(opened.Messages())})
		return
	case len(parts) == 2 && parts[1] == "messages" && r.Method == http.MethodPost:
		var input struct {
			Message      string `json:"message"`
			ReplyContext string `json:"replyContext,omitempty"`
		}
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if readErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": readErr.Error()})
			return
		}
		if json.Unmarshal(body, &input) != nil || strings.TrimSpace(input.Message) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, no-store")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
			flush, _ := w.(http.Flusher)
			emit := func(event string, value any) {
				_, _ = io.WriteString(w, "event: "+event+"\ndata: "+mustJSON(value)+"\n\n")
				if flush != nil {
					flush.Flush()
				}
			}
			_, runErr := h.Dashboard.SendStreamWithEventsAndReply(r.Context(), id, input.Message, input.ReplyContext, func(text string) {
				emit("delta", map[string]string{"text": text})
			}, func(event agent.Event) {
				switch event.Type {
				case "tool_call":
					emit("tool_call", map[string]any{"id": event.ID, "name": event.Name, "args": event.Args})
				case "tool_result":
					emit("tool_result", map[string]any{"id": event.ID, "name": event.Name, "result": event.Result, "isError": event.IsError})
				case "usage":
					emit("usage", map[string]any{"input": event.Usage.Input, "output": event.Usage.Output, "totalTokens": event.Usage.TotalTokens, "cost": 0})
				}
			})
			if runErr != nil {
				_, _ = io.WriteString(w, "event: error\ndata: "+mustJSON(map[string]string{"message": runErr.Error()})+"\n\n")
				return
			}
			_, _ = io.WriteString(w, "event: done\ndata: {}\n\n")
			if flush != nil {
				flush.Flush()
			}
			return
		}
		result, runErr := h.Dashboard.SendWithReply(r.Context(), id, input.Message, input.ReplyContext)
		if runErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": runErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"text": result.FinalText})
	case len(parts) == 2 && parts[1] == "stop" && r.Method == http.MethodPost:
		turn, ok := h.Dashboard.Service.Runner.Active(id)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": false})
			return
		}
		if err := h.Dashboard.Service.Runner.Cancel(turn.ID); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": true, "turnId": turn.ID})
	case len(parts) == 1 && r.Method == http.MethodGet:
		link, ok := findDashboardConversation(h.Dashboard.Service.Registry, id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		session, openErr := h.Dashboard.Service.Runner.OpenSession(link)
		if openErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": openErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session": map[string]any{"id": id, "channel": "dashboard", "title": dashboardSessionTitle(session.Messages(), id), "messageCount": len(session.Messages()), "leafId": session.LeafID()},
			"history": dashboardHistory(session.Messages()),
			"tree":    session.Tree(),
			"runtime": dashboardRuntime(session.Messages()),
			// Keep the early Go pilot response available to non-UI clients.
			"conversationId": id,
			"messages":       session.Messages(),
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func dashboardSessionTitle(messages []session.Message, fallback string) string {
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		title := strings.TrimSpace(contentText(message.Content))
		if title == "" {
			continue
		}
		runes := []rune(title)
		if len(runes) > 80 {
			return string(runes[:80])
		}
		return title
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func dashboardHistory(messages []session.Message) []map[string]any {
	history := make([]map[string]any, 0)
	for _, message := range messages {
		if message.Role == "user" {
			segments := []map[string]any{{"type": "text", "text": contentText(message.Content)}}
			segments = appendImageSegments(segments, message.Images)
			history = append(history, map[string]any{"role": "user", "segments": segments})
			continue
		}
		if message.Role == "assistant" {
			turn := map[string]any{"role": "assistant", "segments": assistantSegments(message.Content), "usage": usageSummary(message.Usage)}
			history = append(history, turn)
			continue
		}
		if message.Role == "toolResult" {
			segment := map[string]any{"type": "tool_result", "id": message.ToolCallID, "name": "", "result": contentText(message.Content), "isError": strings.HasPrefix(contentText(message.Content), "Tool error:")}
			segments := []map[string]any{segment}
			segments = appendImageSegments(segments, message.Images)
			if len(history) > 0 && history[len(history)-1]["role"] == "assistant" {
				history[len(history)-1]["segments"] = append(history[len(history)-1]["segments"].([]map[string]any), segments...)
			}
			continue
		}
		if message.Role == "bashExecution" {
			history = append(history, map[string]any{"role": "bash", "segments": []map[string]any{{
				"type": "bash", "command": message.Command, "result": message.Output,
				"excludeFromContext": message.ExcludeFromContext, "cancelled": message.Cancelled,
			}}})
		}
	}
	return history
}

func assistantSegments(content any) []map[string]any {
	parts, ok := content.([]session.ContentPart)
	if !ok {
		return []map[string]any{{"type": "text", "text": contentText(content)}}
	}
	segments := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		if part.Type == "toolCall" {
			args, _ := part.Arguments.(map[string]any)
			segments = append(segments, map[string]any{"type": "tool_call", "id": part.ID, "name": part.Name, "args": args})
		} else if part.Type == "text" {
			segments = append(segments, map[string]any{"type": "text", "text": part.Text})
		} else if part.Type == "thinking" {
			segments = append(segments, map[string]any{"type": "thinking", "text": part.Text})
		}
	}
	return segments
}

func appendImageSegments(segments []map[string]any, images []string) []map[string]any {
	for _, image := range images {
		if strings.HasPrefix(image, "data:image/") {
			segments = append(segments, map[string]any{"type": "image", "src": image})
		}
	}
	return segments
}

func contentText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	parts, ok := content.([]session.ContentPart)
	if !ok {
		return fmt.Sprint(content)
	}
	var result strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			result.WriteString(part.Text)
		}
	}
	return result.String()
}

func usageSummary(usage *session.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	return map[string]any{"input": usage.Input, "output": usage.Output, "totalTokens": usage.TotalTokens, "cost": 0}
}

func dashboardRuntime(messages []session.Message) map[string]any {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return map[string]any{"provider": nullableString(messages[i].Provider), "modelId": nullableString(messages[i].Model), "thinkingLevel": "", "lastUsage": usageSummary(messages[i].Usage)}
		}
	}
	return map[string]any{"provider": nil, "modelId": nil, "thinkingLevel": "", "lastUsage": nil}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mustJSON(value any) string { data, _ := json.Marshal(value); return string(data) }

var _ http.Handler = DashboardHTTP{}

const dashboardHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Yen dashboard</title><style>
:root{color-scheme:dark;font:15px system-ui,sans-serif}body{margin:0;background:#101214;color:#e9edf1}button,input,textarea{font:inherit}button{cursor:pointer}#app{display:grid;grid-template-columns:250px 1fr;min-height:100vh}aside{border-right:1px solid #2b3036;padding:18px}main{max-width:900px;width:100%;margin:auto;padding:24px;box-sizing:border-box}.brand{font-weight:700;font-size:20px;margin-bottom:18px}button{border:1px solid #39414b;border-radius:7px;background:#1b222a;color:inherit;padding:8px 10px}button:hover{background:#26313c}.new{width:100%;margin-bottom:14px}.session{display:block;width:100%;text-align:left;margin:5px 0}.session.active{border-color:#73b7ff}.branch{display:block;width:100%;text-align:left;margin:5px 0}.meta{display:block;color:#9ba8b5;font-size:12px;margin-top:3px}.bar{display:flex;gap:8px;align-items:center;border-bottom:1px solid #2b3036;padding-bottom:14px}.bar h1{font-size:20px;flex:1;margin:0}.messages{min-height:55vh;padding:18px 0}.turn{border:1px solid #2b3036;border-radius:9px;padding:12px;margin:10px 0;white-space:pre-wrap}.turn.user{background:#172431}.turn.assistant{background:#171a1e}.tool{color:#9ba8b5;font-size:12px;border-left:3px solid #687786;padding-left:8px;margin-top:8px}.compose{display:flex;gap:8px}.compose textarea{flex:1;min-height:52px;resize:vertical;background:#171a1e;color:inherit;border:1px solid #39414b;border-radius:7px;padding:10px}.login{max-width:360px;margin:18vh auto;padding:24px;border:1px solid #2b3036;border-radius:10px}.login input{box-sizing:border-box;width:100%;margin:10px 0;padding:10px;background:#171a1e;color:inherit;border:1px solid #39414b;border-radius:7px}.error{color:#ff9b9b;margin-top:10px}
</style></head><body><section id="login" class="login"><h1>Yen</h1><p>Dashboard access</p><form><input id="token" type="password" autocomplete="current-password" placeholder="Access token"><button>Sign in</button></form><div id="error" class="error"></div></section>
<section id="app" hidden><aside><div class="brand">Yen</div><button id="new" class="new">＋ New session</button><div id="sessions"></div></aside><main><div class="bar"><h1 id="title">No session</h1><button id="stop" hidden>Stop</button></div><div id="branches"></div><div id="messages" class="messages"></div><div id="reply-bar" hidden></div><form id="compose" class="compose"><textarea id="prompt" placeholder="Message Yen…"></textarea><button>Send</button></form></main></section>
<script>
const state={sessions:[],active:null,history:[],tree:[],reply:null};const $=id=>document.getElementById(id);const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
async function api(path,options){const r=await fetch(path,options);const d=await r.json().catch(()=>({}));if(!r.ok)throw Error(d.error||r.status);return d}
function renderSessions(){ $('sessions').innerHTML=state.sessions.map(s=>'<button class="session '+(state.active&&s.id===state.active.id?'active':'')+'" data-id="'+esc(s.id)+'">'+esc(s.title||s.id)+'<span class="meta">'+esc(s.channel||'')+'</span></button>').join('');document.querySelectorAll('[data-id]').forEach(b=>b.onclick=()=>openSession(b.dataset.id)) }
function renderTree(){const entries=state.tree||[],depth=new Map();entries.forEach(e=>{depth.set(e.id,e.parentId&&depth.has(e.parentId)?depth.get(e.parentId)+1:0)});$('branches').innerHTML=entries.filter(e=>e.message&&e.id!==state.active?.leafId).map(e=>'<button class="branch" data-entry="'+esc(e.id)+'" style="padding-left:'+(8+(depth.get(e.id)||0)*16)+'px">Use '+esc(String(e.message.content||'').slice(0,40)||e.type)+'</button>').join('');document.querySelectorAll('.branch').forEach(b=>b.onclick=async()=>{try{const d=await api('/api/sessions/'+encodeURIComponent(state.active.id)+'/branch',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({entryId:b.dataset.entry})});state.tree=d.tree||[];state.active.leafId=d.leafId;renderTree();renderHistory(d.history)}catch(err){alert(err.message)}})}
function flatText(t){return (t.segments||[]).filter(s=>s.type==='text').map(s=>s.text||'').join('')}
function setReply(t){state.reply=t||null;const bar=$('reply-bar');if(!state.reply){bar.hidden=true;bar.textContent='';return}bar.hidden=false;bar.textContent='Replying to: '+flatText(state.reply).slice(0,120);const clear=document.createElement('button');clear.textContent='×';clear.onclick=()=>setReply(null);bar.append(' ',clear)}
function renderHistory(history){state.history=history||state.history;$('messages').innerHTML=state.history.map((t,i)=>{let body=(t.segments||[]).map(s=>s.type==='text'?esc(s.text):s.type==='thinking'?'<div class="tool thinking">'+esc(s.text)+'</div>':s.type==='bash'?'<div class="tool bash"><b>!'+esc(s.command||'')+'</b>'+(s.result?' — '+esc(s.result):'')+(s.excludeFromContext?' <small>(excluded)</small>':'')+'</div>':s.type==='image'?'<img class="attachment" src="'+esc(s.src)+'" alt="generated image">':'<div class="tool">'+esc(s.name||'tool')+(s.result?' — '+esc(s.result):'')+'</div>').join('');return '<article class="turn '+esc(t.role)+'"><b>'+esc(t.role==='user'?'You':t.role==='bash'?'Bash':'Yen')+'</b><div>'+body+'</div><button class="reply" data-reply="'+i+'">Reply</button></article>'}).join('')||'<p>No messages yet.</p>';document.querySelectorAll('.reply').forEach(b=>b.onclick=()=>setReply(state.history[Number(b.dataset.reply)]));$('messages').scrollTop=$('messages').scrollHeight}
async function openSession(id){const d=await api('/api/sessions/'+encodeURIComponent(id));state.active=d.session;state.tree=d.tree||[];setReply(null);$('title').textContent=d.session.title||id;$('stop').hidden=true;renderSessions();renderTree();renderHistory(d.history)}
async function load(){const d=await api('/api/sessions');state.sessions=d.sessions||[];renderSessions();if(!state.active&&state.sessions[0])await openSession(state.sessions[0].id)}
$('login').querySelector('form').onsubmit=async e=>{e.preventDefault();try{await api('/api/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:$('token').value})});$('login').hidden=true;$('app').hidden=false;await load()}catch(err){$('error').textContent=err.message}};
$('new').onclick=async()=>{try{await api('/api/sessions',{method:'POST'});state.active=null;await load()}catch(err){alert(err.message)}};
async function stream(response,fn){const reader=response.body.getReader(),decoder=new TextDecoder();let buffer='';for(;;){const part=await reader.read();if(part.done)return;buffer+=decoder.decode(part.value,{stream:true});let end;while((end=buffer.indexOf('\n\n'))>=0){const frame=buffer.slice(0,end);buffer=buffer.slice(end+2);let name='message',data='';frame.split('\n').forEach(line=>{if(line.startsWith('event:'))name=line.slice(6).trim();if(line.startsWith('data:'))data+=line.slice(5).trim()});if(data)fn(name,JSON.parse(data))}}}
$('compose').onsubmit=async e=>{e.preventDefault();if(!state.active||!$('prompt').value.trim())return;const message=$('prompt').value,id=state.active.id,replyContext=state.reply?flatText(state.reply):undefined;setReply(null);$('prompt').value='';$('stop').hidden=false;const live={role:'assistant',segments:[],active:true};state.history.push({role:'user',segments:[{type:'text',text:message}]},live);renderHistory();try{const response=await fetch('/api/sessions/'+encodeURIComponent(id)+'/messages',{method:'POST',headers:{'Content-Type':'application/json','Accept':'text/event-stream'},body:JSON.stringify({message,replyContext})});if(!response.ok||!response.body)throw Error('stream request failed ('+response.status+')');await stream(response,(name,data)=>{if(name==='delta'){const last=live.segments[live.segments.length-1];if(last&&last.type==='text')last.text+=data.text;else live.segments.push({type:'text',text:data.text})}else if(name==='tool_call')live.segments.push({type:'tool_call',id:data.id,name:data.name,args:data.args});else if(name==='tool_result')live.segments.push({type:'tool_result',id:data.id,name:data.name,result:data.result,isError:data.isError});else if(name==='done')live.active=false;else if(name==='error'){live.active=false;alert(data.message)}renderHistory()});await openSession(id)}catch(err){live.active=false;renderHistory();alert(err.message)}finally{$('stop').hidden=true}};
$('stop').onclick=async()=>{if(state.active)await api('/api/sessions/'+encodeURIComponent(state.active.id)+'/stop',{method:'POST'})};
</script></body></html>`
