package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/charmbracelet/crush-agent/internal/agent"
)

type Handler struct {
	agent *agent.Agent
}

func NewHandler(a *agent.Agent) *Handler {
	return &Handler{agent: a}
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/api/chat", h.handleChat)
	mux.HandleFunc("/api/sessions", h.handleSessions)
	mux.HandleFunc("/api/sessions/", h.handleSessionDetail)
	mux.HandleFunc("/api/busy/", h.handleBusy)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tmpl := template.Must(template.New("index").Parse(indexHTML))
	tmpl.Execute(w, nil)
}

// writeEvent writes a newline-delimited JSON event to the response stream.
// Format: {"event":"type","data":{...}}\n — easy to parse, no SSE complexity.
func writeEvent(w http.ResponseWriter, event string, data any) {
	raw, _ := json.Marshal(map[string]any{"event": event, "data": data})
	fmt.Fprintf(w, "%s\n", raw)
	w.(http.Flusher).Flush()
}

func (h *Handler) handleChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		req.SessionID = "default"
	}
	if req.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// Wire event callback through context (per-request, no global state).
	ctx = agent.WithEventCallback(ctx, func(evt agent.AgentEvent) {
		switch evt.Type {
		case agent.EventToolCall, agent.EventToolResult:
			writeEvent(w, evt.Type, evt.Data)
		case agent.EventLLMStart:
			writeEvent(w, "thinking", map[string]string{"status": "start"})
		case agent.EventResponse:
			writeEvent(w, "response", evt.Data)
		case agent.EventError:
			writeEvent(w, "error", evt.Data)
		case agent.EventDone:
			writeEvent(w, "done", map[string]string{})
		}
	})

	_, err := h.agent.Run(ctx, req.SessionID, req.Message)
	if err != nil {
		writeEvent(w, "error", map[string]string{"message": err.Error()})
		writeEvent(w, "done", map[string]string{})
	}
}

func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	sessions := h.agent.GetSessionManager().List()
	if sessions == nil {
		sessions = []string{}
	}
	data := make([]map[string]any, len(sessions))
	for i, sid := range sessions {
		s := h.agent.GetSessionManager().Get(sid)
		turns := 0
		title := sid
		if s != nil {
			turns = s.TurnCount
			if s.Title != "" {
				title = s.Title
			} else if len(s.Messages) > 0 && s.Messages[0].Role == "user" {
				msg := s.Messages[0].Content
				if len(msg) > 30 {
					msg = msg[:30] + "..."
				}
				title = msg
			}
		}
		data[i] = map[string]any{"id": sid, "turns": turns, "title": title}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	sessionID := r.URL.Path[len("/api/sessions/"):]
	s := h.agent.GetSessionManager().Get(sessionID)
	if s == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]agent.DisplayMsg{})
		return
	}
	msgs := s.MessagesForDisplay()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msgs)
}

func (h *Handler) handleBusy(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Path[len("/api/busy/"):]
	busy := h.agent.GetSessionManager().IsBusy(sessionID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"busy": busy})
}

func RunServer(addr string, h *Handler) error {
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	log.Printf("🌐 momo AI 助手启动 at http://%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

var indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>momo</title>
<script src="https://cdn.bootcdn.net/ajax/libs/marked/5.1.2/marked.min.js"></script>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Noto Sans SC', sans-serif; background: #f5f5f5; height: 100vh; display: flex; color: #1a1a2e; -webkit-font-smoothing: antialiased; }
  .sidebar { width: 240px; background: #1a1a2e; color: #b0b0d0; padding: 20px; display: flex; flex-direction: column; flex-shrink: 0; }
  .sidebar h2 { font-size: 12px; font-weight: 600; margin-bottom: 16px; color: rgba(255,255,255,0.4); letter-spacing: 0.8px; text-transform: uppercase; }
  .session-list { flex: 1; overflow-y: auto; }
  .session-item { padding: 8px 12px; border-radius: 6px; cursor: pointer; margin-bottom: 2px; font-size: 13px; transition: background .12s; }
  .session-item:hover { background: rgba(255,255,255,.05); }
  .session-item.active { background: rgba(124,124,255,.12); border-left: 2px solid #7c7cff; }
  .session-item .sid { font-weight: 500; color: #d0d0e8; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 200px; }
  .new-session-btn { margin-top: 10px; padding: 8px; border: 1px dashed rgba(255,255,255,.1); border-radius: 6px; background: transparent; color: rgba(255,255,255,.3); cursor: pointer; font-size: 12px; transition: all .12s; }
  .new-session-btn:hover { border-color: rgba(124,124,255,.4); color: #7c7cff; }
  .main { flex: 1; display: flex; flex-direction: column; background: #fff; min-width: 0; height: 100vh; }
  .header { padding: 14px 0; border-bottom: 1px solid #f0f0f0; text-align: center; flex-shrink: 0; }
  .header h1 { font-size: 15px; font-weight: 600; color: #1a1a2e; letter-spacing: -.3px; }
  .messages { flex: 1; overflow-y: auto; display: flex; flex-direction: column; }
  .msgs-inner { max-width: 860px; width: 100%; margin: 0 auto; padding: 24px 24px 0; }
  .msg { margin-bottom: 20px; display: flex; }
  .msg.user { justify-content: flex-end; }
  .msg .row { display: flex; align-items: flex-start; gap: 10px; max-width: 85%; }
  .msg.user .row { flex-direction: row-reverse; }
  .msg .avatar { width: 30px; height: 30px; border-radius: 50%; display: flex; align-items: center; justify-content: center; flex-shrink: 0; font-weight: 600; font-size: 13px; }
  .msg.user .avatar { background: #1a1a2e; color: #fff; }
  .msg.assistant .avatar { background: linear-gradient(135deg, #7c7cff, #6a6ae0); color: #fff; font-style: italic; }
  .msg .bubble { padding: 10px 16px; border-radius: 18px; line-height: 1.65; font-size: 14px; white-space: pre-wrap; word-break: break-word; min-width: 0; }
  .msg.user .bubble { background: #7c7cff; color: #fff; border-bottom-right-radius: 5px; }
  .msg.assistant .bubble { background: #f5f5f8; color: #1a1a2e; border-bottom-left-radius: 5px; }
  .msg .bubble p { margin: .4em 0; }
  .msg .bubble p:first-child { margin-top: 0; }
  .msg .bubble p:last-child { margin-bottom: 0; }
  .msg .bubble code { background: rgba(0,0,0,.06); padding: 1px 5px; border-radius: 4px; font-size: 13px; font-family: 'SF Mono', 'Fira Code', monospace; }
  .msg .bubble pre { background: #1a1a2e; color: #e8e8f0; padding: 14px; border-radius: 10px; overflow-x: auto; font-size: 13px; line-height: 1.5; margin: 8px 0; max-height: 500px; white-space: pre-wrap; word-break: break-word; }
  .msg .bubble pre code { background: none; padding: 0; color: inherit; font-size: inherit; white-space: pre-wrap; }
  .msg.tool .bubble { background: #fffbe6; color: #8c6a00; font-size: 13px; border-bottom-left-radius: 5px; }
  .think-wrap { margin: 4px 0 12px; padding-left: 4px; border-left: 3px solid #d0d0e8; }
  .think-block { border: none; border-radius: 0; padding: 2px 0; background: transparent; }
  .think-block > summary { font-size: 13px !important; color: #888 !important; cursor: pointer; user-select: none; padding: 4px 0; }
  .think-block > summary:hover { color: #666 !important; }
  .think-block > summary::before { content: '💭 '; font-size: 12px; }
  .think-steps { padding: 2px 0 2px 12px; }
  .think-steps details { margin: 2px 0; font-size: 12px; color: #888; }
  .think-steps details > summary { font-size: 12px; color: #999; cursor: pointer; user-select: none; padding: 2px 4px; border-radius: 4px; }
  .think-steps details > summary:hover { background: #f5f5fa; color: #666; }
  .think-steps details[open] > summary { color: #666; }
  .think-steps .step-detail { margin-top: 2px; padding: 4px 8px; font-size: 12px; line-height: 1.3; color: #666; background: #fafafe; border-radius: 4px; white-space: pre-wrap; word-break: break-all; max-height: 2.6em; overflow: hidden; position: relative; }
  .think-steps .step-detail.flow { white-space: nowrap; overflow: hidden; max-height: 2.6em; }
  .think-steps .step-detail.flow > .flow-inner { display: inline-block; animation: flow-text 10s linear infinite; padding-left: 100%; }
  @keyframes flow-text { 0% { transform: translateX(0); } 100% { transform: translateX(-100%); } }
  .input-wrap { padding: 16px 24px; border-top: 1px solid #f0f0f0; flex-shrink: 0; }
  .input-inner { max-width: 860px; margin: 0 auto; display: flex; gap: 8px; }
  .input-inner textarea { flex:1; padding:10px 14px; border:1px solid #e0e0e0; border-radius:10px; resize:none; font-size:14px; font-family:inherit; outline:none; transition: border .12s; line-height:1.5; min-height:44px; max-height:200px; }
  .input-inner textarea:focus { border-color: #7c7cff; }
  .input-inner button { padding: 10px 20px; background: #7c7cff; color: #fff; border: none; border-radius: 10px; cursor: pointer; font-size: 14px; font-weight: 500; transition: background .12s; flex-shrink: 0; }
  .input-inner button:hover { background: #6a6ae0; }
  .input-inner button:disabled { background: #c0c0d0; cursor: not-allowed; }
  .thinking-text { color: #999; font-size: 13px; padding: 4px 0; }
  ::-webkit-scrollbar { width: 6px; }
  ::-webkit-scrollbar-track { background: transparent; }
  ::-webkit-scrollbar-thumb { background: #ccc; border-radius: 3px; }
</style>
</head>
<body>
<div class="sidebar">
  <h2>会话</h2>
  <div class="session-list" id="session-list"><div class="session-empty">加载中...</div></div>
  <button class="new-session-btn" onclick="newSession()">+ 新建会话</button>
</div>
<div class="main">
  <div class="header"><h1>momo</h1></div>
  <div class="messages"><div class="msgs-inner" id="msgs-inner"><div style="text-align:center;color:#999;padding:40px;font-size:14px;">选择一个会话开始对话</div></div></div>
  <div class="input-wrap"><div class="input-inner">
    <textarea id="input" rows="1" placeholder="输入消息..." onkeydown="if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();send()}"></textarea>
    <button id="send-btn" onclick="send()">发送</button>
  </div></div>
</div>
<script>
let currentSession = localStorage.getItem('momo_cur') || 'default';
let loadingSessions = {};

function esc(s) { if(!s) return ''; return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

function md(s) { try { return marked.parse(esc(s)); } catch(e) { return esc(s); } }

function updateSendBtn() {
  const btn = document.getElementById('send-btn');
  if(!btn) return;
  if(loadingSessions[currentSession]) {
    btn.disabled = true;
    return;
  }
  // Check backend busy state for cross-tab prevention.
  fetch('/api/busy/'+encodeURIComponent(currentSession)).then(r=>r.json()).then(d => {
    btn.disabled = d.busy === true;
  }).catch(()=>{ btn.disabled = false; });
}

// Poll busy state every 500ms to update button across tabs.
setInterval(updateSendBtn, 500);

function listS() {
  fetch('/api/sessions').then(r=>r.json()).then(sessions => {
    if(!sessions||!sessions.length) sessions=[{id:'default',title:'',turns:0}];
    document.getElementById('session-list').innerHTML = sessions.map(s =>
      '<div class="session-item'+(s.id===currentSession?' active':'')+'" onclick="switchS(\''+s.id+'\')"><div class="sid">'+esc(s.title||s.id)+'</div></div>'
    ).join('');
  }).catch(()=>{});
}

function switchS(id) {
  currentSession=id; localStorage.setItem('momo_cur',id);
  listS(); renderMs(); updateSendBtn();
}

function newSession() {
  currentSession = 's-' + Date.now();
  localStorage.setItem('momo_cur', currentSession);
  renderMs(); listS();
}

function renderMs() {
  const el=document.getElementById('msgs-inner');
  fetch('/api/sessions/'+encodeURIComponent(currentSession)).then(r=>r.json()).then(msgs => {
    if(!msgs||!msgs.length) {
      el.innerHTML = '<div style="text-align:center;color:#999;padding:40px;font-size:14px;">发送一条消息开始对话</div>';
      return;
    }
    let html = '';
    msgs.forEach(m => {
      const avatar = m.role==='user'?'U':'M';
      if(m.type==='thinking') {
        html += '<div class="think-wrap"><details class="think-block"><summary>'+esc(m.summary)+'</summary><div class="think-steps">'+m.content+'</div></details></div>';
      } else {
        const cls = m.role==='user'?'user':m.role==='tool'?'tool':'assistant';
        const content = m.role==='tool'?esc(m.content):md(m.content);
        html += '<div class="msg '+cls+'"><div class="row"><div class="avatar">'+avatar+'</div><div class="bubble">'+content+'</div></div></div>';
      }
    });
    el.innerHTML = html;
    document.querySelectorAll('.think-block').forEach(d => d.open = false);
    if(msgs.length && msgs[msgs.length-1].role==='user') {
      el.innerHTML += '<div class="think-wrap"><div class="think-block" style="border-left-color:#c0c0e8"><div style="font-size:13px;color:#999;padding:4px 0">思考中...</div></div></div>';
    }
    el.scrollIntoView({behavior:'smooth',block:'end'});
  }).catch(()=>{});
}

// Streaming chat via SSE — no polling.
function send() {
  const sid = currentSession;
  if(loadingSessions[sid]) return;
  // Check backend busy state (cross-tab prevention).
  fetch('/api/busy/'+encodeURIComponent(sid)).then(r=>r.json()).then(d => {
    if(d.busy) { updateSendBtn(); return; }
    doSend(sid);
  }).catch(() => doSend(sid));
}

function doSend(sid) {
  const input=document.getElementById('input');
  const msg=input.value.trim();
  if(!msg) return;
  input.value='';
  loadingSessions[sid]=true;
  updateSendBtn();

  const el=document.getElementById('msgs-inner');
  if(el.innerHTML.includes('发送一条消息')||el.innerHTML.includes('选择一个会话')) el.innerHTML='';
  el.innerHTML += '<div class="msg user"><div class="row"><div class="avatar">U</div><div class="bubble">'+esc(msg)+'</div></div></div>';
  el.innerHTML += '<div class="think-wrap" id="stream-think"><div class="think-block" style="border-left-color:#c0c0e8"><div class="thinking-text">思考中...</div></div></div>';
  el.scrollIntoView({behavior:'smooth',block:'end'});

  // Track steps from SSE events.
  const steps = [];
  function renderSteps() {
    if(!steps.length) return;
    let h = '<details class="think-block" open><summary>'+esc('思考过程 ('+steps.length+'步)')+'</summary><div class="think-steps">';
    steps.forEach((s, i) => {
      const isLatest = i === steps.length - 1;
      h += '<details'+(isLatest?' open':'')+'><summary>'+esc(s.icon+' '+s.label)+'</summary><div class="step-detail'+(isLatest?' flow':'')+'">'+(isLatest?'<span class="flow-inner">'+esc(s.detail)+'</span>':esc(s.detail))+'</div></details>';
    });
    h += '</div></details>';
    const tw = document.getElementById('stream-think');
    if(tw) { tw.innerHTML = h; tw.scrollIntoView({behavior:'smooth',block:'end'}); }
  }

  // Read SSE stream from POST response.
  fetch('/api/chat', {
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({session_id:sid,message:msg})
  }).then(async r => {
    const reader = r.body.getReader();
    const dec = new TextDecoder();
    let buf = '';

    while(true) {
      const {done, value} = await reader.read();
      if(done) break;
      buf += dec.decode(value, {stream: true});

      // NDJSON: each complete line is a JSON object {event, data}
      const lines = buf.split('\n');
      buf = lines.pop() || ''; // keep incomplete line for next chunk

      for(const line of lines) {
        if(!line.trim()) continue;
        try {
          const msg = JSON.parse(line);
          const evt = msg.event;
          const d = msg.data;

          if(evt==='tool_call') {
            steps.push({icon:'🔧', label:d.name+'('+(d.args||'').slice(0,60)+')', detail:d.args||''});
            renderSteps();
          } else if(evt==='tool_result') {
            if(steps.length) {
              const last=steps[steps.length-1];
              last.icon='📎'; last.detail=d.result||'';
            } else {
              steps.push({icon:'📎', label:'tool result', detail:d.result||''});
            }
            renderSteps();
          } else if(evt==='thinking') {
            if(steps.length && steps[steps.length-1].icon!=='🤔') {
              steps.push({icon:'🤔', label:'思考中...', detail:''});
              renderSteps();
            }
          } else if(evt==='response') {
            // Final response — renderMs handles it on 'done'.
          } else if(evt==='error') {
            const tw=document.getElementById('stream-think');
            if(tw) tw.innerHTML = '<div class="think-block" style="border-left-color:#e88"><div style="font-size:13px;color:#c00;padding:4px 0">错误: '+esc(d.message||'')+'</div></div>';
          } else if(evt==='done') {
            if(currentSession===sid) renderMs();
            listS();
          }
        } catch(e) {}
      }
    }
  }).catch(e => {
    const tw=document.getElementById('stream-think');
    if(tw) tw.innerHTML = '<div class="think-block" style="border-left-color:#e88"><div style="font-size:13px;color:#c00;padding:4px 0">请求失败: '+esc(e.message)+'</div></div>';
  }).finally(() => {
    delete loadingSessions[sid];
    updateSendBtn();
  });
}

listS();
renderMs();
</script>
</body>
</html>`
