// 用 libmpv 的最小宿主程序，verify.sh 用它证明 libmpv.2.dylib 能经 @rpath 装载、能初始化，
// 并且 render API（OpenGL，nagare 窗口里嵌 mpv 走的就是这条路）真的能把画面画出来。
//
//   embed_test                    建句柄 → config=no、vo=libmpv → 初始化 → 退出
//   embed_test --render <文件>    在离屏 CGL 上下文里建 render context，播 <文件> 并把若干帧画进
//                                 FBO，读回像素确认不是空白。CGL 上下文建不起来（没有可用的 GL）
//                                 时以 77 退出，表示「跳过」
//
// 编译：clang -arch arm64 -arch x86_64 -mmacosx-version-min=13.0 -I<产物>/include embed_test.c
//       -L<产物>/Frameworks -lmpv.2 -framework OpenGL -Wl,-rpath,@executable_path/../Frameworks
#define GL_SILENCE_DEPRECATION 1

#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include <OpenGL/OpenGL.h>
#include <OpenGL/gl3.h>

#include <mpv/client.h>
#include <mpv/render_gl.h>

#define SKIPPED 77
#define WIDTH 160
#define HEIGHT 96
#define FRAMES_WANTED 30
#define TIMEOUT_SECONDS 30.0

static int fail(const char *what, int err) {
    fprintf(stderr, "embed_test: %s failed: %s\n", what, err < 0 ? mpv_error_string(err) : "");
    return 1;
}

static int set_opt(mpv_handle *h, const char *name, const char *value) {
    int err = mpv_set_option_string(h, name, value);
    if (err < 0) {
        fprintf(stderr, "embed_test: option %s=%s: %s\n", name, value, mpv_error_string(err));
    }
    return err;
}

static double now_seconds(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec + ts.tv_nsec / 1e9;
}

#if defined(__aarch64__)
#define THIS_ARCH "arm64"
#elif defined(__x86_64__)
#define THIS_ARCH "x86_64"
#else
#define THIS_ARCH "unknown"
#endif

static int basic(void) {
    unsigned long api = mpv_client_api_version();
    printf("arch=%s client API %lu.%lu\n", THIS_ARCH, api >> 16, api & 0xffff);
    mpv_handle *h = mpv_create();
    if (!h) {
        return fail("mpv_create", 0);
    }
    if (set_opt(h, "config", "no") < 0 || set_opt(h, "vo", "libmpv") < 0 ||
        set_opt(h, "idle", "yes") < 0 || set_opt(h, "terminal", "no") < 0) {
        return 1;
    }
    int err = mpv_initialize(h);
    if (err < 0) {
        return fail("mpv_initialize", err);
    }
    char *version = mpv_get_property_string(h, "mpv-version");
    char *ffmpeg = mpv_get_property_string(h, "ffmpeg-version");
    char *vo = mpv_get_property_string(h, "vo");
    printf("mpv-version=%s ffmpeg-version=%s vo=%s\n", version ? version : "?",
           ffmpeg ? ffmpeg : "?", vo ? vo : "?");
    int ok = version && vo && strcmp(vo, "libmpv") == 0;
    mpv_free(version);
    mpv_free(ffmpeg);
    mpv_free(vo);
    const char *quit[] = {"quit", NULL};
    mpv_command(h, quit);
    mpv_terminate_destroy(h);
    if (!ok) {
        fprintf(stderr, "embed_test: unexpected property values\n");
        return 1;
    }
    printf("embed_test: basic OK\n");
    return 0;
}

static void *get_proc_address(void *ctx, const char *name) {
    (void)ctx;
    static void *gl;
    if (!gl) {
        gl = dlopen("/System/Library/Frameworks/OpenGL.framework/OpenGL", RTLD_LAZY | RTLD_LOCAL);
    }
    return gl ? dlsym(gl, name) : NULL;
}

static CGLContextObj make_gl_context(void) {
    CGLPixelFormatAttribute attempts[2][8] = {
        {kCGLPFAOpenGLProfile, (CGLPixelFormatAttribute)kCGLOGLPVersion_3_2_Core,
         kCGLPFAColorSize, (CGLPixelFormatAttribute)24, kCGLPFAAccelerated,
         kCGLPFAAllowOfflineRenderers, (CGLPixelFormatAttribute)0},
        // 没有显卡时（虚拟机）退到苹果的软件渲染器
        {kCGLPFAOpenGLProfile, (CGLPixelFormatAttribute)kCGLOGLPVersion_3_2_Core,
         kCGLPFARendererID, (CGLPixelFormatAttribute)kCGLRendererGenericFloatID,
         (CGLPixelFormatAttribute)0},
    };
    for (int i = 0; i < 2; i++) {
        CGLPixelFormatObj pix = NULL;
        GLint npix = 0;
        if (CGLChoosePixelFormat(attempts[i], &pix, &npix) != kCGLNoError || !pix) {
            continue;
        }
        CGLContextObj ctx = NULL;
        CGLError e = CGLCreateContext(pix, NULL, &ctx);
        CGLDestroyPixelFormat(pix);
        if (e == kCGLNoError && ctx) {
            printf("GL context: %s\n", i == 0 ? "accelerated" : "software (GenericFloat)");
            return ctx;
        }
    }
    return NULL;
}

static int render(const char *file, const char *hwdec) {
    CGLContextObj ctx = make_gl_context();
    if (!ctx) {
        fprintf(stderr, "embed_test: no usable OpenGL context, skipping render test\n");
        return SKIPPED;
    }
    CGLSetCurrentContext(ctx);
    printf("GL_RENDERER=%s GL_VERSION=%s\n", glGetString(GL_RENDERER), glGetString(GL_VERSION));

    GLuint tex, fbo;
    glGenTextures(1, &tex);
    glBindTexture(GL_TEXTURE_2D, tex);
    glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA8, WIDTH, HEIGHT, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
    glGenFramebuffers(1, &fbo);
    glBindFramebuffer(GL_FRAMEBUFFER, fbo);
    glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, tex, 0);
    if (glCheckFramebufferStatus(GL_FRAMEBUFFER) != GL_FRAMEBUFFER_COMPLETE) {
        fprintf(stderr, "embed_test: framebuffer incomplete\n");
        return 1;
    }

    mpv_handle *h = mpv_create();
    if (!h) {
        return fail("mpv_create", 0);
    }
    if (set_opt(h, "config", "no") < 0 || set_opt(h, "vo", "libmpv") < 0 ||
        set_opt(h, "ao", "null") < 0 || set_opt(h, "hwdec", hwdec) < 0 ||
        set_opt(h, "keep-open", "yes") < 0 || set_opt(h, "terminal", "yes") < 0 ||
        set_opt(h, "msg-level", "all=warn,vo=info,vd=info") < 0) {
        return 1;
    }
    int err = mpv_initialize(h);
    if (err < 0) {
        return fail("mpv_initialize", err);
    }

    mpv_opengl_init_params gl_init = {.get_proc_address = get_proc_address};
    mpv_render_param params[] = {
        {MPV_RENDER_PARAM_API_TYPE, MPV_RENDER_API_TYPE_OPENGL},
        {MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &gl_init},
        {MPV_RENDER_PARAM_INVALID, NULL},
    };
    mpv_render_context *rctx = NULL;
    err = mpv_render_context_create(&rctx, h, params);
    if (err < 0) {
        return fail("mpv_render_context_create", err);
    }

    const char *load[] = {"loadfile", file, NULL};
    err = mpv_command(h, load);
    if (err < 0) {
        return fail("loadfile", err);
    }

    int frames = 0, result = 1;
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
        if (ev->event_id == MPV_EVENT_END_FILE) {
            mpv_event_end_file *end = ev->data;
            if (end->reason == MPV_END_FILE_REASON_ERROR) {
                fprintf(stderr, "embed_test: playback error: %s\n", mpv_error_string(end->error));
                goto done;
            }
        }
        if (mpv_render_context_update(rctx) & MPV_RENDER_UPDATE_FRAME) {
            err = mpv_render_context_render(rctx, rparams);
            if (err < 0) {
                fail("mpv_render_context_render", err);
                goto done;
            }
            mpv_render_context_report_swap(rctx);
            frames++;
        }
    }

    static unsigned char pixels[WIDTH * HEIGHT * 4];
    glBindFramebuffer(GL_FRAMEBUFFER, fbo);
    glReadPixels(0, 0, WIDTH, HEIGHT, GL_RGBA, GL_UNSIGNED_BYTE, pixels);
    long sum = 0;
    int distinct = 0;
    unsigned char first[3] = {pixels[0], pixels[1], pixels[2]};
    for (int i = 0; i < WIDTH * HEIGHT; i++) {
        sum += pixels[i * 4] + pixels[i * 4 + 1] + pixels[i * 4 + 2];
        if (memcmp(&pixels[i * 4], first, 3) != 0) {
            distinct++;
        }
    }
    char *cur = mpv_get_property_string(h, "hwdec-current");
    char *vo = mpv_get_property_string(h, "current-vo");
    printf("frames=%d hwdec-current=%s current-vo=%s mean=%.1f differing-pixels=%d\n", frames,
           cur ? cur : "?", vo ? vo : "?", sum / (3.0 * WIDTH * HEIGHT), distinct);
    mpv_free(cur);
    mpv_free(vo);
    // testsrc2 是彩色测试图：画出来的帧应当有大量互不相同的像素
    if (frames < FRAMES_WANTED) {
        fprintf(stderr, "embed_test: only %d frames rendered before timeout\n", frames);
    } else if (distinct < WIDTH * HEIGHT / 4) {
        fprintf(stderr, "embed_test: rendered image looks blank\n");
    } else {
        printf("embed_test: render OK\n");
        result = 0;
    }

done:
    mpv_render_context_free(rctx);
    mpv_terminate_destroy(h);
    CGLSetCurrentContext(NULL);
    CGLDestroyContext(ctx);
    return result;
}

int main(int argc, char **argv) {
    if (argc == 1) {
        return basic();
    }
    if (argc >= 3 && strcmp(argv[1], "--render") == 0) {
        return render(argv[2], argc >= 4 ? argv[3] : "no");
    }
    fprintf(stderr, "usage: embed_test [--render <file> [hwdec]]\n");
    return 2;
}
