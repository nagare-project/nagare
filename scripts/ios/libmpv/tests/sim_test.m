// verify.sh --simulator 用的最小宿主程序：链接 xcframework 里模拟器那一份 Libmpv.framework，
// 经 `xcrun simctl spawn` 在 iOS 模拟器里跑。证明这个库在 iOS 上能装载、能初始化、能解码、
// Lua 能用且没有 JIT，render API 能在 OpenGL ES 上把画面画出来。
//
//   sim_test info                      打印版本与协议清单
//   sim_test play <文件或 URL>          vo=null / ao=null 不计时解码到 1.5 秒处，视频与音频都要解出东西来
//   sim_test lua <脚本> <文件>          加载 <脚本>，把 mpv 的日志原样打印出来（verify.sh 在里面找脚本的输出）
//   sim_test render <文件> <hwdec>      EAGLContext（OpenGL ES）+ FBO 上建 render context，画 30 帧读回像素
//
// 退出码：0 通过；1 失败；2 用法不对；3 播放以错误结束（verify.sh 用它确认 https:// 被拒）；
//         77 没有可用的 OpenGL ES 上下文（跳过）
//
// 编译（verify.sh 里）：clang -target <架构>-apple-ios15.0-simulator -isysroot <模拟器 SDK> -fobjc-arc
//   -F <xcframework>/ios-arm64_x86_64-simulator -framework Libmpv -framework OpenGLES -framework Foundation
//   -Wl,-rpath,<xcframework>/ios-arm64_x86_64-simulator sim_test.m
#define GLES_SILENCE_DEPRECATION 1

#import <Foundation/Foundation.h>
#import <OpenGLES/EAGL.h>
#import <OpenGLES/ES3/gl.h>

#include <dlfcn.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

#include <Libmpv/mpv/client.h>
#include <Libmpv/mpv/render_gl.h>

enum { OK = 0, FAILED = 1, USAGE = 2, PLAYBACK_ERROR = 3, SKIPPED = 77 };
#define WIDTH 160
#define HEIGHT 96
#define FRAMES_WANTED 30
#define TIMEOUT_SECONDS 60.0

#if defined(__aarch64__)
#define THIS_ARCH "arm64"
#elif defined(__x86_64__)
#define THIS_ARCH "x86_64"
#else
#define THIS_ARCH "unknown"
#endif

static double now_seconds(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec + ts.tv_nsec / 1e9;
}

static int set_opt(mpv_handle *h, const char *name, const char *value) {
    int err = mpv_set_option_string(h, name, value);
    if (err < 0) {
        fprintf(stderr, "sim_test: option %s=%s: %s\n", name, value, mpv_error_string(err));
    }
    return err;
}

// 公共选项 + 初始化。日志一律打到标准输出（simctl spawn 会转发回来），并记下有没有解码器报错。
static mpv_handle *make_handle(const char *const *opts) {
    mpv_handle *h = mpv_create();
    if (!h) {
        fprintf(stderr, "sim_test: mpv_create failed\n");
        return NULL;
    }
    const char *base[] = {"config", "no", "terminal", "no", "idle", "no", NULL};
    for (int i = 0; base[i]; i += 2) {
        if (set_opt(h, base[i], base[i + 1]) < 0) return NULL;
    }
    for (int i = 0; opts && opts[i]; i += 2) {
        if (set_opt(h, opts[i], opts[i + 1]) < 0) return NULL;
    }
    int err = mpv_initialize(h);
    if (err < 0) {
        fprintf(stderr, "sim_test: mpv_initialize: %s\n", mpv_error_string(err));
        return NULL;
    }
    mpv_request_log_messages(h, "info");
    return h;
}

static int decoder_error = 0;

static void print_log(mpv_event *ev) {
    mpv_event_log_message *msg = ev->data;
    printf("[%s] %s: %s", msg->level, msg->prefix, msg->text);
    if (strstr(msg->text, "Failed to initialize a decoder") || strstr(msg->text, "Could not open codec")) {
        decoder_error = 1;
    }
}

static int info(void) {
    mpv_handle *h = make_handle((const char *[]){"vo", "null", "ao", "null", NULL});
    if (!h) return FAILED;
    char *version = mpv_get_property_string(h, "mpv-version");
    char *ffmpeg = mpv_get_property_string(h, "ffmpeg-version");
    char *protocols = mpv_get_property_string(h, "protocol-list");
    printf("arch=%s mpv-version=%s ffmpeg-version=%s\nprotocol-list=%s\n", THIS_ARCH,
           version ? version : "?", ffmpeg ? ffmpeg : "?", protocols ? protocols : "?");
    int ok = version && ffmpeg && protocols;
    mpv_free(version);
    mpv_free(ffmpeg);
    mpv_free(protocols);
    mpv_terminate_destroy(h);
    return ok ? OK : FAILED;
}

// 不计时地解码到 1.5 秒处。视频宽度与音频采样率都观察到非零值、以 EOF 结束、没有解码器报错，才算通过。
static int play(const char *file, const char *script) {
    const char *opts[] = {"vo", "null", "ao", "null", "hwdec", "no", "untimed", "yes", "ao-null-untimed", "yes",
                          "end", "1.5", script ? "scripts" : NULL, script, NULL};
    mpv_handle *h = make_handle(opts);
    if (!h) return FAILED;
    mpv_observe_property(h, 1, "video-params/w", MPV_FORMAT_INT64);
    mpv_observe_property(h, 2, "audio-params/samplerate", MPV_FORMAT_INT64);
    const char *load[] = {"loadfile", file, NULL};
    int err = mpv_command(h, load);
    if (err < 0) {
        fprintf(stderr, "sim_test: loadfile: %s\n", mpv_error_string(err));
        return FAILED;
    }
    int64_t width = 0, rate = 0;
    int result = FAILED, done = 0;
    double deadline = now_seconds() + TIMEOUT_SECONDS;
    while (!done && now_seconds() < deadline) {
        mpv_event *ev = mpv_wait_event(h, 1.0);
        switch (ev->event_id) {
        case MPV_EVENT_LOG_MESSAGE:
            print_log(ev);
            break;
        case MPV_EVENT_PROPERTY_CHANGE: {
            mpv_event_property *p = ev->data;
            if (p->format == MPV_FORMAT_INT64) {
                int64_t v = *(int64_t *)p->data;
                if (ev->reply_userdata == 1 && v > width) width = v;
                if (ev->reply_userdata == 2 && v > rate) rate = v;
            }
            break;
        }
        case MPV_EVENT_END_FILE: {
            mpv_event_end_file *end = ev->data;
            done = 1;
            if (end->reason == MPV_END_FILE_REASON_ERROR) {
                printf("sim_test: playback error: %s\n", mpv_error_string(end->error));
                result = PLAYBACK_ERROR;
            } else if (script) {
                result = OK; // lua 模式：脚本自己 quit（原因是 quit 不是 eof），结论由 verify.sh 看日志
            } else if (end->reason != MPV_END_FILE_REASON_EOF) {
                printf("sim_test: playback ended with reason %d\n", end->reason);
            } else if (width <= 0 || rate <= 0 || decoder_error) {
                printf("sim_test: video width=%lld audio samplerate=%lld decoder-error=%d\n", (long long)width,
                       (long long)rate, decoder_error);
            } else {
                printf("sim_test: play OK width=%lld samplerate=%lld\n", (long long)width, (long long)rate);
                result = OK;
            }
            break;
        }
        case MPV_EVENT_SHUTDOWN:
            done = 1;
            if (script) result = OK;
            break;
        default:
            break;
        }
    }
    if (!done) printf("sim_test: timeout\n");
    // 把剩下的日志（脚本 quit 之后的那几行）也打出来
    for (;;) {
        mpv_event *ev = mpv_wait_event(h, 0);
        if (ev->event_id == MPV_EVENT_NONE || ev->event_id == MPV_EVENT_SHUTDOWN) break;
        if (ev->event_id == MPV_EVENT_LOG_MESSAGE) print_log(ev);
    }
    mpv_terminate_destroy(h);
    return result;
}

static void *get_proc_address(void *ctx, const char *name) {
    (void)ctx;
    return dlsym(RTLD_DEFAULT, name);
}

static int render(const char *file, const char *hwdec) {
    EAGLContext *gl = [[EAGLContext alloc] initWithAPI:kEAGLRenderingAPIOpenGLES3];
    if (!gl) gl = [[EAGLContext alloc] initWithAPI:kEAGLRenderingAPIOpenGLES2];
    if (!gl || ![EAGLContext setCurrentContext:gl]) {
        fprintf(stderr, "sim_test: no usable OpenGL ES context, skipping render test\n");
        return SKIPPED;
    }
    printf("GL_RENDERER=%s GL_VERSION=%s\n", (const char *)glGetString(GL_RENDERER),
           (const char *)glGetString(GL_VERSION));

    GLuint rb, fbo;
    glGenRenderbuffers(1, &rb);
    glBindRenderbuffer(GL_RENDERBUFFER, rb);
    glRenderbufferStorage(GL_RENDERBUFFER, gl.API == kEAGLRenderingAPIOpenGLES3 ? GL_RGBA8 : GL_RGBA4, WIDTH,
                          HEIGHT);
    glGenFramebuffers(1, &fbo);
    glBindFramebuffer(GL_FRAMEBUFFER, fbo);
    glFramebufferRenderbuffer(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_RENDERBUFFER, rb);
    if (glCheckFramebufferStatus(GL_FRAMEBUFFER) != GL_FRAMEBUFFER_COMPLETE) {
        fprintf(stderr, "sim_test: framebuffer incomplete\n");
        return FAILED;
    }

    // 模拟器的 GL 是软件实现，mpv 认出来会默认拒绝（gpu-sw=no）：这里明知如此，放行
    const char *opts[] = {"vo", "libmpv", "ao", "null", "hwdec", hwdec, "keep-open", "yes", "gpu-sw", "yes", NULL};
    mpv_handle *h = make_handle(opts);
    if (!h) return FAILED;
    mpv_opengl_init_params gl_init = {.get_proc_address = get_proc_address};
    mpv_render_param params[] = {
        {MPV_RENDER_PARAM_API_TYPE, MPV_RENDER_API_TYPE_OPENGL},
        {MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &gl_init},
        {MPV_RENDER_PARAM_INVALID, NULL},
    };
    mpv_render_context *rctx = NULL;
    int err = mpv_render_context_create(&rctx, h, params);
    if (err < 0) {
        fprintf(stderr, "sim_test: mpv_render_context_create: %s\n", mpv_error_string(err));
        return FAILED;
    }
    const char *load[] = {"loadfile", file, NULL};
    if ((err = mpv_command(h, load)) < 0) {
        fprintf(stderr, "sim_test: loadfile: %s\n", mpv_error_string(err));
        return FAILED;
    }

    int frames = 0, result = FAILED;
    double deadline = now_seconds() + TIMEOUT_SECONDS;
    mpv_opengl_fbo target = {.fbo = (int)fbo, .w = WIDTH, .h = HEIGHT};
    int flip = 1;
    mpv_render_param rparams[] = {
        {MPV_RENDER_PARAM_OPENGL_FBO, &target},
        {MPV_RENDER_PARAM_FLIP_Y, &flip},
        {MPV_RENDER_PARAM_INVALID, NULL},
    };
    while (frames < FRAMES_WANTED && now_seconds() < deadline) {
        mpv_event *ev = mpv_wait_event(h, 0.005);
        if (ev->event_id == MPV_EVENT_LOG_MESSAGE) {
            print_log(ev);
        } else if (ev->event_id == MPV_EVENT_END_FILE) {
            mpv_event_end_file *end = ev->data;
            if (end->reason == MPV_END_FILE_REASON_ERROR) {
                fprintf(stderr, "sim_test: playback error: %s\n", mpv_error_string(end->error));
                goto done;
            }
        }
        if (mpv_render_context_update(rctx) & MPV_RENDER_UPDATE_FRAME) {
            if ((err = mpv_render_context_render(rctx, rparams)) < 0) {
                fprintf(stderr, "sim_test: mpv_render_context_render: %s\n", mpv_error_string(err));
                goto done;
            }
            mpv_render_context_report_swap(rctx);
            frames++;
        }
    }

    static unsigned char pixels[WIDTH * HEIGHT * 4];
    glBindFramebuffer(GL_FRAMEBUFFER, fbo);
    glReadPixels(0, 0, WIDTH, HEIGHT, GL_RGBA, GL_UNSIGNED_BYTE, pixels);
    int distinct = 0;
    for (int i = 0; i < WIDTH * HEIGHT; i++) {
        if (memcmp(&pixels[i * 4], pixels, 3) != 0) distinct++;
    }
    char *cur = mpv_get_property_string(h, "hwdec-current");
    printf("frames=%d hwdec-current=%s differing-pixels=%d\n", frames, cur ? cur : "?", distinct);
    mpv_free(cur);
    // 测试片段是彩色测试图：画出来的帧应当有大量互不相同的像素
    if (frames < FRAMES_WANTED) {
        fprintf(stderr, "sim_test: only %d frames rendered before timeout\n", frames);
    } else if (distinct < WIDTH * HEIGHT / 4) {
        fprintf(stderr, "sim_test: rendered image looks blank\n");
    } else {
        printf("sim_test: render OK\n");
        result = OK;
    }

done:
    mpv_render_context_free(rctx);
    mpv_terminate_destroy(h);
    [EAGLContext setCurrentContext:nil];
    return result;
}

int main(int argc, char **argv) {
    @autoreleasepool {
        setvbuf(stdout, NULL, _IOLBF, 0);
        if (argc == 2 && strcmp(argv[1], "info") == 0) return info();
        if (argc == 3 && strcmp(argv[1], "play") == 0) return play(argv[2], NULL);
        if (argc == 4 && strcmp(argv[1], "lua") == 0) return play(argv[3], argv[2]);
        if (argc == 4 && strcmp(argv[1], "render") == 0) return render(argv[2], argv[3]);
        fprintf(stderr, "usage: sim_test info | play <file> | lua <script> <file> | render <file> <hwdec>\n");
        return USAGE;
    }
}
