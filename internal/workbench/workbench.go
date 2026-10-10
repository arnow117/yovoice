// Package workbench 是应用服务层：编排作品、素材、角色、模型、运行时与生成。
// 方法可被并发调用：短事务由编辑锁串行化，后台操作（下载、安装、推理）同一时间只运行一个。
package workbench

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
	"yovoice/internal/diag"
	"yovoice/internal/download"
	"yovoice/internal/engine"
	"yovoice/internal/msg"
	"yovoice/internal/recognition"
	"yovoice/internal/schema"
	"yovoice/internal/store"
)

type Workbench struct {
	Store *store.Store
	// edit 串行化“读取状态 → 校验引用 → 写入”的短事务，避免并发调用使引用检查失效；
	// 持有期间不执行转码、下载或推理等耗时操作。锁顺序：先 edit 后 mu。
	edit sync.Mutex
	// mu 保护后台操作的取消函数与完成通道。
	mu                sync.Mutex
	cancel            context.CancelFunc
	done              chan struct{}
	active            *operation
	closed            bool
	closeOnce         sync.Once
	engine            *engine.Engine
	client            *http.Client
	speakerConfigOnce sync.Once
	speakerConfig     recognition.Config
	speakerConfigErr  error
	// beforeSpeakerCommit 仅供受控测试在登记的最终接受点之前同步。
	beforeSpeakerCommit func()
	// afterGenerationSaved 仅供受控测试在历史记录已写入、操作最终状态尚未收尾时同步。
	afterGenerationSaved func()
	// beforeSpeakerResultReturn 仅供受控测试在 profile 操作完成后、调用方取得结果前同步。
	beforeSpeakerResultReturn func()
	// bundled 记录随安装包分发的内核，键为后端；启动时登记，之后只读。
	bundled map[string]bundledRuntime
}

// operation 持有一次后台操作自己的取消、收尾与最终错误，避免已完成请求影响后续请求。
type operation struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func (o *operation) finish(err error) {
	o.mu.Lock()
	o.err = err
	o.mu.Unlock()
}

func (o *operation) Err() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

func New(root string) (*Workbench, error) {
	s, e := store.New(root)
	if e != nil {
		return nil, e
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		preferences := s.Read().Preferences
		if (preferences.ProxyEnabled == nil && preferences.ProxyURL == "") || (preferences.ProxyEnabled != nil && !*preferences.ProxyEnabled) {
			return nil, nil
		}
		return download.Proxy(req, preferences.ProxyURL)
	}
	w := &Workbench{Store: s, engine: engine.New(root), client: &http.Client{Transport: transport}, bundled: map[string]bundledRuntime{}}
	w.removeStale()
	if e = w.recordLegacyRuntimeVersion(); e != nil {
		return nil, e
	}
	return w, nil
}

// 前台命令由服务串行调度，耗时操作独立运行，状态通过 Store 广播。
func (w *Workbench) begin(kind string, code msg.Code, params msg.Params, modelID *string, action func(context.Context) error, requestIDs ...string) error {
	_, err := w.start(kind, code, params, modelID, action, requestIDs...)
	return err
}

// start 启动后台操作并返回本次操作的完成通道，调用方据此等待，不会误等后续操作。
func (w *Workbench) start(kind string, code msg.Code, params msg.Params, modelID *string, action func(context.Context) error, requestIDs ...string) (*operation, error) {
	return w.startOperation(kind, code, params, modelID, action, false, requestIDs...)
}

func (w *Workbench) startProfileOperation(kind string, code msg.Code, params msg.Params, modelID *string, action func(context.Context) error, requestIDs ...string) (*operation, error) {
	return w.startOperation(kind, code, params, modelID, action, true, requestIDs...)
}

func (w *Workbench) startOperation(kind string, code msg.Code, params msg.Params, modelID *string, action func(context.Context) error, redactError bool, requestIDs ...string) (*operation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.cancel != nil {
		return nil, msg.Err(msg.ErrBusy, nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	update := func(s *schema.State) {
		requestID := ""
		if len(requestIDs) > 0 {
			requestID = requestIDs[0]
		}
		s.Activity = &schema.Activity{RequestID: requestID, Kind: kind, Code: code, Params: params, Status: "running", ModelID: modelID, StartedAt: time.Now().UTC()}
		if len(requestIDs) > 1 {
			if requestID == "" {
				s.Activity.ProjectID = requestIDs[1]
			} else {
				s.Activity.CharacterID = requestIDs[1]
			}
		}
	}
	updateState := w.Store.Update
	if redactError {
		updateState = w.Store.UpdateProfile
	}
	if e := updateState(update, true); e != nil {
		cancel()
		if redactError {
			return nil, msg.Err(msg.ErrStateSaveFailed, nil)
		}
		return nil, e
	}
	operationID := schema.NewID()
	started := time.Now()
	fields := []any{"operation_id", operationID, "kind", kind, "model_id", value(modelID), "related_ids", requestIDs}
	diag.Log(w.Store.Root, "operation.started", fields...)
	op := &operation{cancel: cancel, done: make(chan struct{})}
	w.cancel = op.cancel
	w.done = op.done
	w.active = op
	go func() {
		defer close(op.done)
		defer cancel()
		err := action(ctx)
		if redactError && err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			err = msg.Err(msg.ErrGenerateFailed, nil)
		} else if !redactError && err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			var callError *msg.CallError
			if !errors.As(err, &callError) {
				err = msg.Err(msg.ErrUnknown, msg.Params{"detail": err.Error()})
			}
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		update := func(s *schema.State) {
			a := s.Activity
			if err == nil {
				a.Code = msg.ActivityCompleted
				a.Params = nil
				a.Status = "completed"
				a.ErrorCode = nil
				a.ErrorParams = nil
			} else if errors.Is(err, context.Canceled) {
				a.Status = "cancelled"
				a.Code = msg.ActivityCancelled
				a.Params = nil
				if kind == "download" {
					a.Code = msg.ActivityPaused
				}
				a.ErrorCode = nil
				a.ErrorParams = nil
			} else {
				a.Status = "failed"
				a.Code = msg.ActivityFailed
				a.Params = nil
				if ce, ok := err.(*msg.CallError); ok && ce != nil {
					a.ErrorCode = &ce.Code
					a.ErrorParams = ce.Params
				} else {
					code := msg.ErrUnknown
					a.ErrorCode = &code
					a.ErrorParams = msg.Params{"detail": err.Error()}
				}
			}
		}
		if e := updateState(update, true); e != nil {
			_ = updateState(func(s *schema.State) {
				update(s)
				s.Activity.Status = "failed"
				code := msg.ErrStateSaveFailed
				s.Activity.ErrorCode = &code
				if redactError {
					s.Activity.ErrorParams = nil
				} else {
					s.Activity.ErrorParams = msg.Params{"detail": e.Error()}
				}
			}, false)
			if redactError {
				err = msg.Err(msg.ErrStateSaveFailed, nil)
			} else {
				err = msg.Err(msg.ErrStateSaveFailed, msg.Params{"detail": e.Error()})
			}
		}
		status := "completed"
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		} else if err != nil {
			status = "failed"
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			_ = os.WriteFile(filepath.Join(w.Store.Root, "logs", "last-error.txt"), []byte(err.Error()), 0600)
		}
		diag.Log(w.Store.Root, "operation.finished", append(fields, "status", status, "elapsed_ms", time.Since(started).Milliseconds(), "error", diag.Error(err))...)
		op.finish(err)
		if w.active == op {
			w.cancel = nil
			w.active = nil
		}
	}()
	return op, nil
}

// Done 返回当前操作的完成信号；没有进行中的操作时返回已关闭的通道。
func (w *Workbench) Done() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return w.done
}

func (w *Workbench) Cancel() {
	diag.Log(w.Store.Root, "operation.cancel_requested")
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
	}
}
func (w *Workbench) Close() {
	w.closeOnce.Do(func() {
		diag.Log(w.Store.Root, "service.stopping")
		defer diag.Log(w.Store.Root, "service.stopped")
		w.mu.Lock()
		w.closed = true
		if w.cancel != nil {
			w.cancel()
		}
		done := w.done
		w.mu.Unlock()
		if done != nil {
			<-done
		}
		w.engine.Stop()
		w.client.CloseIdleConnections()
	})
}

func (w *Workbench) progress(code msg.Code, params msg.Params, received, total int64) {
	_ = w.Store.Update(func(s *schema.State) {
		if s.Activity != nil {
			s.Activity.Code = code
			s.Activity.Params = params
			s.Activity.Received = received
			s.Activity.Total = total
		}
	}, false)
}
