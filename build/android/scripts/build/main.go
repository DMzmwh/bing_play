//go:build !android

// Command build 是 Android 构建流水线的跨平台入口，供 build/android/Taskfile.yml 调用。
//
// 它取代了原先只能运行在 bash + unix 宿主上的内联脚本：
//   - shared：使用 NDK 编译 libwails.so（go build -buildmode=c-shared）
//   - apk / aab：调用 Gradle 打包，并把产物复制到 bin/
//   - clean：清理构建产物
//
// 这样 Windows 宿主也能直接执行 android 任务，无需 WSL 或 Git Bash。
package main

import (
	"bytes"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// defaultTVSourceURL 与数据库迁移中的默认订阅源保持一致，用于打包时更新内置频道数据。
const defaultTVSourceURL = "https://raw.githubusercontent.com/cs3306/IPTV-Sources/main/data/output/iptv_collection.m3u"

// bundledTVPlaylist 内置频道兜底文件，随二进制嵌入，订阅源不可达时使用。
const bundledTVPlaylist = "resources/tv/bundled.m3u"

func main() {
	var (
		mode       string
		arch       string
		minSDK     int
		production bool
		appName    string
		binDir     string
	)
	flag.StringVar(&mode, "mode", "shared", "shared|bundle|apk|aab|clean")
	flag.StringVar(&arch, "arch", "arm64", "arm64|amd64")
	flag.IntVar(&minSDK, "min-sdk", 21, "NDK clang 包装脚本使用的 API level")
	flag.BoolVar(&production, "production", true, "生产构建（release 变体）；false 为 debug")
	flag.StringVar(&appName, "name", "bing-play", "产物基础名，输出为 <name>.apk / <name>.aab")
	flag.StringVar(&binDir, "bin-dir", "bin", "产物输出目录")
	flag.Parse()

	root, err := projectRoot()
	if err != nil {
		fail(err)
	}

	switch mode {
	case "shared":
		err = compileShared(root, arch, minSDK, production)
	case "bundle":
		err = bundleTVPlaylist(root)
	case "apk", "aab":
		err = assemble(root, mode, production, appName, binDir)
	case "clean":
		err = clean(root, binDir)
	default:
		err = fmt.Errorf("未知的 -mode %q，可选 shared / bundle / apk / aab / clean", mode)
	}
	if err != nil {
		fail(err)
	}
}

// bundleTVPlaylist 只更新内置频道数据：优先本地数据库，其次默认订阅源。
func bundleTVPlaylist(root string) error {
	if exportBundledFromDB(root) {
		return nil
	}
	updateBundledTVPlaylist(root)
	return nil
}

// compileShared 使用 NDK 把主程序编译成 Android 共享库。
func compileShared(root, arch string, minSDK int, production bool) error {
	// 优先使用本地数据库已同步的频道，其次尝试抓取默认订阅源。
	if err := bundleTVPlaylist(root); err != nil {
		return err
	}

	sdk, err := androidSDK()
	if err != nil {
		return err
	}
	ndk, err := androidNDK(sdk)
	if err != nil {
		return err
	}

	hostTag := map[string]string{
		"darwin":  "darwin-x86_64",
		"linux":   "linux-x86_64",
		"windows": "windows-x86_64",
	}[runtime.GOOS]
	if hostTag == "" {
		return fmt.Errorf("不支持的构建宿主平台: %s", runtime.GOOS)
	}

	var triple, goarch, jniDir string
	switch arch {
	case "arm64":
		triple, goarch, jniDir = "aarch64-linux-android", "arm64", "arm64-v8a"
	case "amd64", "x86_64":
		triple, goarch, jniDir = "x86_64-linux-android", "amd64", "x86_64"
	default:
		return fmt.Errorf("不支持的架构 %q，可选 arm64 / amd64", arch)
	}

	toolchain := filepath.Join(ndk, "toolchains", "llvm", "prebuilt", hostTag)
	// Windows 的 NDK 工具链是 .cmd 包装脚本。
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".cmd"
	}
	cc := filepath.Join(toolchain, "bin", fmt.Sprintf("%s%d-clang%s", triple, minSDK, suffix))
	cxx := filepath.Join(toolchain, "bin", fmt.Sprintf("%s%d-clang++%s", triple, minSDK, suffix))
	if _, err := os.Stat(cc); err != nil {
		return fmt.Errorf("找不到 NDK 编译器 %s\n请确认已安装 NDK（ANDROID_NDK_HOME），或执行 sdkmanager \"ndk;26.3.11579264\"", cc)
	}

	overlay := filepath.Join(root, "build", "android", "overlay.json")
	if _, err := os.Stat(overlay); err != nil {
		return fmt.Errorf("缺少 %s，请先执行 wails3 task android:generate:android:overlay", overlay)
	}

	out := filepath.Join(root, "build", "android", "app", "src", "main", "jniLibs", jniDir, "libwails.so")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	args := []string{"build", "-buildmode=c-shared", "-overlay", overlay}
	if production {
		args = append(args, "-tags", "production,android", "-trimpath", "-buildvcs=false", "-ldflags=-w -s")
	} else {
		args = append(args, "-tags", "android,debug", "-buildvcs=false", "-gcflags=all=-l")
	}
	args = append(args, "-o", out)

	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=1",
		"GOOS=android",
		"GOARCH="+goarch,
		"CC="+cc,
		"CXX="+cxx,
		// NDK 的 clang 包装脚本可能需要同目录下的其它工具。
		"PATH="+filepath.Join(toolchain, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	return run(cmd, fmt.Sprintf("编译 %s 共享库", arch))
}

// exportBundledFromDB 从本地数据库导出已同步的频道，作为 TV 首次启动的兜底数据。
// 数据库不存在或没有频道时返回 false，由调用方回退到网络抓取。
func exportBundledFromDB(root string) bool {
	dbPath := os.Getenv("TV_BUNDLED_DB")
	if dbPath == "" {
		dbPath = filepath.Join(root, "data", "db", "bt.db")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return false
	}

	dsn := "file:" + filepath.ToSlash(dbPath) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Printf("读取本地频道数据库失败: %v\n", err)
		return false
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT tvg_id, tvg_name, name, logo, group_title, stream_url, duration
		 FROM tv_channels
		 ORDER BY group_title ASC, sort_order ASC, id ASC`,
	)
	if err != nil {
		fmt.Printf("读取本地频道失败: %v\n", err)
		return false
	}
	defer rows.Close()

	var builder strings.Builder
	builder.WriteString("#EXTM3U\n")

	count := 0
	seen := make(map[string]struct{})
	for rows.Next() {
		var tvgID, tvgName, name, logo, groupTitle, streamURL string
		var duration int
		if err := rows.Scan(&tvgID, &tvgName, &name, &logo, &groupTitle, &streamURL, &duration); err != nil {
			fmt.Printf("读取本地频道失败: %v\n", err)
			return false
		}
		if name == "" || streamURL == "" {
			continue
		}
		if _, ok := seen[streamURL]; ok {
			continue
		}
		seen[streamURL] = struct{}{}

		fmt.Fprintf(
			&builder,
			"#EXTINF:%d tvg-id=\"%s\" tvg-name=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n%s\n",
			duration, escapeM3UAttr(tvgID), escapeM3UAttr(tvgName), escapeM3UAttr(logo),
			escapeM3UAttr(groupTitle), name, streamURL,
		)
		count++
	}
	if err := rows.Err(); err != nil {
		fmt.Printf("读取本地频道失败: %v\n", err)
		return false
	}
	if count == 0 {
		fmt.Printf("本地数据库没有频道数据: %s\n", dbPath)
		return false
	}

	path := filepath.Join(root, bundledTVPlaylist)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(builder.String()), 0o644); err != nil {
		fmt.Printf("写入内置频道数据失败: %v\n", err)
		return false
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Printf("写入内置频道数据失败: %v\n", err)
		return false
	}

	fmt.Printf("已从本地数据库导出内置频道: %s (%d channels)\n", path, count)
	return true
}

// escapeM3UAttr 转义 M3U 属性值中的双引号。
func escapeM3UAttr(value string) string {
	return strings.ReplaceAll(value, `"`, "'")
}

// updateBundledTVPlaylist 尝试在打包前抓取默认订阅源，作为 TV 首次启动时的频道兜底。
// 抓取失败或内容无效时沿用仓库中已有的 bundled.m3u，不影响构建。
func updateBundledTVPlaylist(root string) {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(defaultTVSourceURL)
	if err != nil {
		fmt.Printf("跳过内置频道更新: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("跳过内置频道更新: HTTP %d\n", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		fmt.Printf("跳过内置频道更新: %v\n", err)
		return
	}
	if !bytes.Contains(body, []byte("#EXTM3U")) {
		fmt.Printf("跳过内置频道更新: 内容不是 M3U 播放列表\n")
		return
	}

	path := filepath.Join(root, bundledTVPlaylist)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		fmt.Printf("写入内置频道数据失败: %v\n", err)
		return
	}
	fmt.Printf("已更新内置频道数据: %s (%d bytes)\n", path, len(body))
}

// assemble 调用 Gradle 打包，并把产物复制到 bin/。
func assemble(root, kind string, production bool, appName, binDir string) error {
	javaHome, err := resolveJavaHome()
	if err != nil {
		return err
	}

	gradleDir := filepath.Join(root, "build", "android")
	gradle := "gradlew"
	if runtime.GOOS == "windows" {
		gradle = "gradlew.bat"
	} else {
		// 从压缩包解出的 gradlew 可能丢失可执行位。
		_ = os.Chmod(filepath.Join(gradleDir, gradle), 0o755)
	}

	variant, task := "debug", "assembleDebug"
	if production {
		variant, task = "release", "assembleRelease"
	}
	if kind == "aab" {
		task = "bundleDebug"
		if production {
			task = "bundleRelease"
		}
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// exec.Command 不会在 cmd.Dir 中查找相对路径，必须用绝对路径。
		cmd = exec.Command(filepath.Join(gradleDir, gradle), task)
	} else {
		cmd = exec.Command("sh", "./"+gradle, task)
	}
	cmd.Dir = gradleDir
	// 只对 Gradle 子进程覆盖 JAVA_HOME，用户全局的 JDK 配置保持不变。
	cmd.Env = append(os.Environ(), "JAVA_HOME="+javaHome)
	if err := run(cmd, "Gradle "+task); err != nil {
		return err
	}

	outputDir, ext := "apk", "apk"
	if kind == "aab" {
		outputDir, ext = "bundle", "aab"
	}
	src := filepath.Join(gradleDir, "app", "build", "outputs", outputDir, variant,
		fmt.Sprintf("app-%s.%s", variant, ext))
	dst := filepath.Join(root, binDir, appName+"."+ext)
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("复制产物失败（%s）: %w", src, err)
	}
	fmt.Printf("产物: %s\n", dst)
	return nil
}

func clean(root, binDir string) error {
	targets := []string{
		filepath.Join(root, binDir),
		filepath.Join(root, "build", "android", "app", "build"),
		filepath.Join(root, "build", "android", ".gradle"),
		filepath.Join(root, "build", "android", ".kotlin"),
	}
	for _, target := range targets {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		fmt.Printf("已清理: %s\n", target)
	}
	return nil
}

// resolveJavaHome 找出 Android 构建专用的 JDK（要求 17+），不影响全局 JAVA_HOME。
// 优先级：ANDROID_JAVA_HOME > 版本满足要求的 JAVA_HOME > PATH 中的 java。
func resolveJavaHome() (string, error) {
	if home := strings.TrimSpace(os.Getenv("ANDROID_JAVA_HOME")); home != "" {
		major, ok := javaMajorAt(home)
		if !ok {
			return "", fmt.Errorf("ANDROID_JAVA_HOME 无效或无法执行 java: %s", home)
		}
		if major < 17 {
			return "", fmt.Errorf("ANDROID_JAVA_HOME 指向 JDK %d，需要 JDK 17+（推荐 21）", major)
		}
		return home, nil
	}

	javaHome := strings.TrimSpace(os.Getenv("JAVA_HOME"))
	if javaHome != "" {
		if major, ok := javaMajorAt(javaHome); ok && major >= 17 {
			return javaHome, nil
		}
	}

	// 回退到 PATH 中的 java；Gradle 仍需要 JAVA_HOME，所以反推安装目录。
	if javaExe, err := exec.LookPath("java"); err == nil {
		if javaMajorOf(javaExe) >= 17 {
			return filepath.Dir(filepath.Dir(javaExe)), nil
		}
	}

	if javaHome != "" {
		if major, ok := javaMajorAt(javaHome); ok {
			return "", fmt.Errorf("当前 JAVA_HOME 是 JDK %d，Android 构建需要 JDK 17+：请设置 ANDROID_JAVA_HOME 指向 JDK 21（不会影响其他项目使用的 JAVA_HOME）", major)
		}
	}
	return "", fmt.Errorf("未找到 JDK 17+：请安装 JDK 21 并设置 ANDROID_JAVA_HOME（例如 D:\\programfiles\\Java\\jdk-21.0.13.1+2）")
}

// javaMajorAt 返回指定 JDK 目录的主版本号。
func javaMajorAt(home string) (int, bool) {
	java := filepath.Join(home, "bin", "java")
	if runtime.GOOS == "windows" {
		java += ".exe"
	}
	if _, err := os.Stat(java); err != nil {
		return 0, false
	}
	major := javaMajorOf(java)
	return major, major > 0
}

func javaMajorOf(javaExe string) int {
	out, err := exec.Command(javaExe, "-version").CombinedOutput()
	if err != nil {
		return 0
	}
	return javaMajorVersion(string(out))
}

// javaMajorVersion 从 `java -version` 输出中解析主版本号，如 1.8.0_291 -> 8、21.0.1 -> 21。
func javaMajorVersion(output string) int {
	idx := strings.Index(output, `version "`)
	if idx < 0 {
		return 0
	}
	version := output[idx+len(`version "`):]
	if end := strings.IndexByte(version, '"'); end >= 0 {
		version = version[:end]
	}

	parts := strings.FieldsFunc(version, func(r rune) bool {
		return r == '.' || r == '_' || r == '+' || r == '-'
	})
	if len(parts) == 0 {
		return 0
	}
	first, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	if first == 1 && len(parts) > 1 {
		if second, err := strconv.Atoi(parts[1]); err == nil {
			return second
		}
	}
	return first
}

func androidSDK() (string, error) {
	for _, key := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if dirExists(v) {
				return v, nil
			}
			return "", fmt.Errorf("%s 指向的目录不存在: %s", key, v)
		}
	}

	home, _ := os.UserHomeDir()
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates, filepath.Join(local, "Android", "Sdk"))
		}
		candidates = append(candidates, filepath.Join(home, "Android", "Sdk"))
	case "darwin":
		candidates = append(candidates, filepath.Join(home, "Library", "Android", "sdk"))
	default:
		candidates = append(candidates, filepath.Join(home, "Android", "Sdk"))
	}

	for _, c := range candidates {
		if dirExists(filepath.Join(c, "ndk")) || dirExists(filepath.Join(c, "platform-tools")) {
			return c, nil
		}
	}
	return "", fmt.Errorf("未找到 Android SDK：请安装 Android Studio（SDK Manager）后设置 ANDROID_HOME，例如 %s",
		strings.Join(candidates, " 或 "))
}

func androidNDK(sdk string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("ANDROID_NDK_HOME")); v != "" {
		if dirExists(v) {
			return v, nil
		}
		return "", fmt.Errorf("ANDROID_NDK_HOME 指向的目录不存在: %s", v)
	}

	entries, err := os.ReadDir(filepath.Join(sdk, "ndk"))
	if err != nil {
		return "", fmt.Errorf("未找到 Android NDK：请通过 SDK Manager 安装 \"NDK (Side by side)\"，或设置 ANDROID_NDK_HOME")
	}

	best, bestVersion := "", []int(nil)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		version := parseVersion(entry.Name())
		if best == "" || compareVersions(version, bestVersion) > 0 {
			best, bestVersion = filepath.Join(sdk, "ndk", entry.Name()), version
		}
	}
	if best == "" {
		return "", fmt.Errorf("%s/ndk 下没有可用的 NDK", sdk)
	}
	return best, nil
}

func parseVersion(s string) []int {
	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		nums = append(nums, n)
	}
	return nums
}

func compareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			return av - bv
		}
	}
	return 0
}

// projectRoot 从当前目录向上查找 go.mod，保证从任意子目录调用都能定位项目根。
func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("未找到项目根目录（go.mod）")
		}
		dir = parent
	}
}

func run(cmd *exec.Cmd, label string) error {
	fmt.Printf("==> %s\n", label)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s 失败: %w", label, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}
