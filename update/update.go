package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ethtweet/ethtweet/global"
	"github.com/ethtweet/ethtweet/logs"
	"github.com/polydawn/refmt/json"
	"golang.org/x/crypto/openpgp"
)

func ChcckGithubVersion() {
	r, err := http.Get("https://api.github.com/repos/ethtweet/ethtweet/releases/latest")
	if err != nil {
		return
	}
	b, err := io.ReadAll(r.Body)
	var v interface{}
	err = json.Unmarshal(b, &v)
	if err != nil {
		logs.PrintErr(err)
		return
	}

	data := v.(map[string]interface{})

	githubVerion := fmt.Sprintf("%s", data["tag_name"])
	githubVerion = strings.Replace(githubVerion, "v", "", 1)
	if compareVersion(githubVerion, global.Version) > 0 {
		logs.PrintlnSuccess("GitHub版本更高")
	} else {
		logs.PrintlnSuccess("不需要升级")
		return
	}

	githubPublishedTime, _ := time.ParseInLocation("2006-01-02T15:04:05Z", fmt.Sprintf("%s", data["published_at"]), time.Local)
	if time.Now().Sub(githubPublishedTime) < (time.Second * 3600) {
		logs.PrintlnSuccess("更新时间不足1个小时，延迟更新")
		return
	}
	updateFileUrl := fmt.Sprintf("https://github.com/ethtweet/ethtweet/releases/download/v%s/EthTweet-%s-%s-%s.zip", githubVerion, githubVerion, runtime.GOOS, runtime.GOARCH)
	// Get the data
	resp, err := http.Get(updateFileUrl)

	if resp.StatusCode == 404 {
		logs.PrintErr("文件不存在，404错误" + updateFileUrl)
		return
	}
	if err != nil {
		logs.PrintErr(err)
		return
	} else {
		logs.PrintlnSuccess("下载最新安装包成功")
	}
	defer resp.Body.Close()

	// 创建一个文件用于保存
	out, err := os.Create("update.zip")
	if err != nil {
		logs.PrintErr(err)
	}
	defer out.Close()

	// 然后将响应流和文件流对接起来
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		logs.PrintErr(err)
		return
	}

	out, err = os.Open("update.zip")
	if err != nil {
		fmt.Println(err)
	}
	h := sha512.New()
	if _, err := io.Copy(h, out); err != nil {
		logs.PrintErr(err)
		return
	}

	fileSha512 := hex.EncodeToString(h.Sum(nil))

	checksumsFileURL := fmt.Sprintf("https://github.com/ethtweet/ethtweet/releases/download/v%s/EthTweet-%s-%s-%s.zip.sha512", githubVerion, githubVerion, runtime.GOOS, runtime.GOARCH)
	r, err = http.Get(checksumsFileURL)
	if err != nil {
		logs.PrintErr(err)
		return
	}
	b, err = io.ReadAll(r.Body)
	checksums := string(b)
	if strings.Index(checksums, fileSha512) < 0 {

		logs.PrintErr("文件sha512错误，下载的文件sha512:" + fileSha512)
		return
	}

	ascFileURL := fmt.Sprintf("https://github.com/ethtweet/ethtweet/releases/download/v%s/EthTweet-%s-%s-%s.zip.asc", githubVerion, githubVerion, runtime.GOOS, runtime.GOARCH)
	err = DownloadFile(ascFileURL, "update.zip.asc")
	if err != nil {
		logs.PrintErr(err)
		return
	}

	Verify, err := VerifySignature("update.zip")
	if err != nil {
		logs.PrintErr(err)
		return
	}
	if !Verify {
		logs.PrintErr("gpg签名不通过")
		return
	}

	exeFilename, _ := os.Executable()

	//删除老文件
	if global.FileExists(path.Base(exeFilename) + ".old") {
		err = os.Remove(path.Base(exeFilename) + ".old")
		if err != nil {
			logs.PrintErr(err)
			return
		}
	}

	err = os.Rename(path.Base(exeFilename), path.Base(exeFilename)+".old")
	if err != nil {
		logs.PrintErr(err)
		return
	}

	err = Unzip("update.zip", ".")
	if err != nil {
		logs.PrintErr(err)
		return
	}

	logs.Println("current version: ", global.Version)
	logs.Println("Update to version: ", githubVerion)
	logs.Println("Ready to restart")
	time.Sleep(time.Second * 5) //更新前休眠5秒，避免重复冲突
	os.Exit(0)
}

func Unzip(zipPath, dstDir string) error {
	// open zip file
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if err := unzipFile(file, dstDir); err != nil {
			return err
		}
	}
	return nil
}

func unzipFile(file *zip.File, dstDir string) error {
	// create the directory of file
	filePath := path.Join(dstDir, file.Name)
	if file.FileInfo().IsDir() {
		if err := os.MkdirAll(filePath, os.ModePerm); err != nil {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
		return err
	}

	// open the file
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	// create the file
	w, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer w.Close()

	w.Chmod(0777)

	// save the decompressed file content
	_, err = io.Copy(w, rc)
	return err
}

func compareVersion(version1 string, version2 string) int {
	var res int
	ver1Strs := strings.Split(version1, ".")
	ver2Strs := strings.Split(version2, ".")
	ver1Len := len(ver1Strs)
	ver2Len := len(ver2Strs)
	verLen := ver1Len
	if len(ver1Strs) < len(ver2Strs) {
		verLen = ver2Len
	}
	for i := 0; i < verLen; i++ {
		var ver1Int, ver2Int int
		if i < ver1Len {
			ver1Int, _ = strconv.Atoi(ver1Strs[i])
		}
		if i < ver2Len {
			ver2Int, _ = strconv.Atoi(ver2Strs[i])
		}
		if ver1Int < ver2Int {
			res = -1
			break
		}
		if ver1Int > ver2Int {
			res = 1
			break
		}
	}
	return res
}

func DownloadFile(url string, dest string) error {
	// Get the data
	resp, err := http.Get(url)

	if resp.StatusCode == 404 {
		logs.PrintErr("文件不存在，404错误")
		return http.ErrMissingFile
	}
	if err != nil {
		logs.PrintErr(err)
		return err
	}
	defer resp.Body.Close()

	// 创建一个文件用于保存
	out, err := os.Create(dest)
	if err != nil {
		logs.PrintErr(err)
	}
	defer out.Close()
	return nil
}

var publicKey = `-----BEGIN PGP PUBLIC KEY BLOCK-----

mQINBGRVILgBEACxqkRKodS2Mfxn6GTYvUDaBSgQCjT/GMqmto38buSing9PCXv6
QMWko8Ax7cKVkxEKGD+4T+AD2mLfhpjLBlMOcxqBwuJ4YVsWkHH2TLHc/gU3DL9Y
ajH9Lt8TF+Xin/pBfGdOBXGeKK2Az8RshK5D3w3E89//plL15kaR0BWbVIp6Ne0P
c5D7BNboRuqJGAY+aYEipWAHLZW5M2dD1wgVjUpZRwWv+qIKuQ+hri+fxehFjz3S
8ElwqZu8JQHxcO3b3m3j11x1qfekqRvNf/dxMpuS+ymenAjOmDDlarmSTj9RTzrA
97uYi2meIr5e85yMNk5n8Ks7HOQyQ1K6J7YBodjItO7bp1EE5xSecNsaIT2kBQX3
0+uga0IsZkA6MIC8caWfkMIXrdyLse4XFywCdOGI3BhrA6QV/7ZAXRBs5HtO6SQO
eVfDptZ0VCvmWG8v6d5mBJ6081FylHEoDYXfJVwgRo71UR334WBpRJZQNV76p383
muUSq05IcwjbAdyol26enqO2s5LRNs7OeISAhQ+u2LV6LJK+G23JKbmIuWD7Rhol
gLDXYukoIlOcY7x++qnqoLT8V1aNFE/4XDAd+/Xq7VdgvKbPZxxEkXj9LMrPBIaS
9/1Nmiq/ni779pnGCFDS7UUFLJvWjEDgWKnZb8MYBdyvq9T9biecJ2oR6wARAQAB
tCJjaGVuamlhNDA0IDxjaGVuamlhYmxvZ0BnbWFpbC5jb20+iQJXBBMBCABBFiEE
4TRiUu1mI2TKN/cWGJvnloM2naMFAmRVILgCGw8FCQPDFwgFCwkIBwICIgIGFQoJ
CAsCBBYCAwECHgcCF4AACgkQGJvnloM2naMQJA/+OxZGpywGLf+C1Wi9iVsSb0UA
Xit9yOujEpgttgJBdZcfP/1W5G7Vlt9pEH1ByJ28RHlSrEdMkycYhmvnDPdCTg+c
x3NtjWP8xWXsWN9upPPnn3ZdtsSDZ2YQOMjunP7mucRW8NofDFytPFgSVb6+NcqM
9Obcd6gmOY3qoQcv4XofdlP6ObFZxvr/mGKdSBgWgOQivGK8QtimNeC/V5ChJKyl
rueQJ1RRnGtlTXW3tNPNmYkXeVR/TVZgVHyIBHjlNHRV7V8Wgm+vsNIo7xPD/PHL
3Kq2pmuz8EpcJpNK1+IYsQJTEx9+Y4E4Vjjp/U6WBjGDWXF5KrdTKMsRsHvhxOW7
C/u6e9gG/eHPLo5Pw3Dg5MWZh/+dRZ/1kWoKhabp719CCPOh9SBgUdSc8RAoVTwp
b/UHSPokJPPlpBWU7mdBJ+fCapswHU8Gg4WnwBrm2C+p7GEXZiJ6f5n2Ic9rVu6x
mkmOANLziPe7kC8T6830d0l2nlyCR/oKoGrQ8+bQNChHHhGWtr4O/uCrES5NK/Xr
kT6OojW8UeV5ngFa0fFurYcMahHHaoy/S3bduGMk3yiFI8Wh7LkZQO6ugkoesvqv
YSCJJrSTjHnBCkddmOHpDpvgOe+COOrVCe42PNSovTJ+14rhMTsYOWShLLOdC02L
/xTrrn8LrU9TVEUWf4I=
=l1Ub
-----END PGP PUBLIC KEY BLOCK-----`

func VerifySignature(filename string) (bool, error) {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader([]byte(publicKey)))
	if err != nil {
		fmt.Println("Read Armored Key Ring: " + err.Error())
		return false, err
	}

	signature, err := os.Open(filename + ".asc")
	if err != nil {
		fmt.Println(err)
		return false, err
	}

	verification_target, err := os.Open(filename)
	if err != nil {
		fmt.Println(err)
		return false, err
	}
	entity, err := openpgp.CheckArmoredDetachedSignature(keyring, verification_target, signature)
	if err != nil {
		fmt.Println("Check Detached Signature: " + err.Error())
		return false, err
	}
	if entity.PrimaryKey.KeyIdString() == "189BE79683369DA3" {
		return true, nil
	} else {
		return false, nil
	}

}
