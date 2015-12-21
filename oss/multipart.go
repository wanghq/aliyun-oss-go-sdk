package oss

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
)

//
// InitiateMultipartUpload 初始化分片上传任务。
//
// objectKey  Object名称。
// options    上传时可以指定Object的属性，可选属性有CacheControl、ContentDisposition、ContentEncoding、Expires、
// ServerSideEncryption、Meta，具体含义请参考
// https://help.aliyun.com/document_detail/oss/api-reference/multipart-upload/InitiateMultipartUpload.html
//
// InitiateMultipartUploadResult 初始化后操作成功的返回值，用于后面的UploadPartFromFile、UploadPartCopy等操作。error为nil时有效。
// error  操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) InitiateMultipartUpload(objectKey string, options ...Option) (InitiateMultipartUploadResult, error) {
	var imur InitiateMultipartUploadResult
	opts := addContentType(options, objectKey)
	resp, err := bucket.do("POST", objectKey, "uploads", "uploads", opts, nil)
	if err != nil {
		return imur, err
	}
	defer resp.body.Close()

	err = xmlUnmarshal(resp.body, &imur)
	return imur, err
}

//
// UploadPart 上传分片。
//
// 初始化一个Multipart Upload之后，可以根据指定的Object名和Upload ID来分片（Part）上传数据。
// 每一个上传的Part都有一个标识它的号码（part number，范围是1~10000）。对于同一个Upload ID，
// 该号码不但唯一标识这一片数据，也标识了这片数据在整个文件内的相对位置。如果您用同一个part号码，上传了新的数据，
// 那么OSS上已有的这个号码的Part数据将被覆盖。除了最后一片Part以外，其他的part最小为100KB；
// 最后一片Part没有大小限制。
//
// imur        InitiateMultipartUpload成功后的返回值。
// reader      io.Reader 需要分片上传的reader。
// size        本次上传片Part的大小。
// partNumber  本次上传片(Part)的编号，范围是1~10000。如果超出范围，OSS将返回InvalidArgument错误。
//
// UploadPart 上传成功的返回值，两个成员PartNumber、ETag。PartNumber片编号，即传入参数partNumber；
// ETag及上传数据的MD5。error为nil时有效。
// error 操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) UploadPart(imur InitiateMultipartUploadResult, reader io.Reader,
	size int64, partNumber int) (UploadPart, error) {
	var part = UploadPart{}
	params := "partNumber=" + strconv.Itoa(partNumber) + "&uploadId=" + imur.UploadID
	opts := []Option{ContentLength(size)}
	resp, err := bucket.do("PUT", imur.Key, params, params, opts, &io.LimitedReader{R: reader, N: size})
	if err != nil {
		return part, err
	}
	defer resp.body.Close()

	part.ETag = resp.headers.Get(HTTPHeaderEtag)
	part.PartNumber = partNumber
	return part, nil
}

//
// UploadPartFromFile 上传分片。
//
// imur           InitiateMultipartUpload成功后的返回值。
// filePath       需要分片上传的本地文件。
// startPosition  本次上传文件片的起始位置。
// partSize       本次上传文件片的大小。
// partNumber     本次上传文件片的编号，范围是1~10000。
//
// UploadPart 上传成功的返回值，两个成员PartNumber、ETag。PartNumber片编号，传入参数partNumber；
// ETag上传数据的MD5。error为nil时有效。
// error 操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) UploadPartFromFile(imur InitiateMultipartUploadResult, filePath string,
	startPosition, partSize int64, partNumber int) (UploadPart, error) {
	var part = UploadPart{}
	fd, err := os.Open(filePath)
	if err != nil {
		return part, err
	}
	defer fd.Close()
	fd.Seek(startPosition, os.SEEK_SET)

	params := "partNumber=" + strconv.Itoa(partNumber) + "&uploadId=" + imur.UploadID
	resp, err := bucket.do("PUT", imur.Key, params, params, nil, &io.LimitedReader{R: fd, N: partSize})
	if err != nil {
		return part, err
	}
	defer resp.body.Close()

	part.ETag = resp.headers.Get(HTTPHeaderEtag)
	part.PartNumber = partNumber
	return part, nil
}

//
// UploadPartCopy 拷贝分片。
//
// imur           InitiateMultipartUpload成功后的返回值。
// copySrc        源Object名称。
// startPosition  本次拷贝片(Part)在源Object的起始位置。
// partSize       本次拷贝片的大小。
// partNumber     本次拷贝片的编号，范围是1~10000。如果超出范围，OSS将返回InvalidArgument错误。
// options        copy时源Object的限制条件，满足限制条件时copy，不满足时返回错误。可选条件有CopySourceIfMatch、
// CopySourceIfNoneMatch、CopySourceIfModifiedSince  CopySourceIfUnmodifiedSince，具体含义请参看
// https://help.aliyun.com/document_detail/oss/api-reference/multipart-upload/UploadPartCopy.html
//
// UploadPart 上传成功的返回值，两个成员PartNumber、ETag。PartNumber片(Part)编号，即传入参数partNumber；
// ETag及上传数据的MD5。error为nil时有效。
// error 操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) UploadPartCopy(imur InitiateMultipartUploadResult, copySrc string, startPosition,
	partSize int64, partNumber int, options ...Option) (UploadPart, error) {
	var out UploadPartCopyResult
	var part UploadPart

	opts := []Option{CopySource(bucket.BucketName, copySrc),
		CopySourceRange(startPosition, partSize)}
	opts = append(opts, options...)
	params := "partNumber=" + strconv.Itoa(partNumber) + "&uploadId=" + imur.UploadID
	resp, err := bucket.do("PUT", imur.Key, params, params, opts, nil)
	if err != nil {
		return part, err
	}
	defer resp.body.Close()

	err = xmlUnmarshal(resp.body, &out)
	if err != nil {
		return part, err
	}
	part.ETag = out.ETag
	part.PartNumber = partNumber

	return part, nil
}

//
// CompleteMultipartUpload 提交分片上传任务。
//
// imur   InitiateMultipartUpload的返回值。
// parts  UploadPart/UploadPartFromFile/UploadPartCopy返回值组成的数组。
//
// CompleteMultipartUploadResponse  操作成功后的返回值。error为nil时有效。
// error  操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) CompleteMultipartUpload(imur InitiateMultipartUploadResult,
	parts []UploadPart) (CompleteMultipartUploadResult, error) {
	var out CompleteMultipartUploadResult

	sort.Sort(uploadParts(parts))
	cxml := completeMultipartUploadXML{}
	cxml.Part = parts
	bs, err := xml.Marshal(cxml)
	if err != nil {
		return out, err
	}
	buffer := new(bytes.Buffer)
	buffer.Write(bs)

	params := "uploadId=" + imur.UploadID
	resp, err := bucket.do("POST", imur.Key, params, params, nil, buffer)
	if err != nil {
		return out, err
	}
	defer resp.body.Close()

	err = xmlUnmarshal(resp.body, &out)
	return out, err
}

//
// AbortMultipartUpload 取消分片上传任务。
//
// imur  InitiateMultipartUpload的返回值。
//
// error  操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) AbortMultipartUpload(imur InitiateMultipartUploadResult) error {
	params := "uploadId=" + imur.UploadID
	resp, err := bucket.do("DELETE", imur.Key, params, params, nil, nil)
	if err != nil {
		return err
	}
	defer resp.body.Close()
	return checkRespCode(resp.statusCode, []int{http.StatusNoContent})
}

//
// ListUploadedParts 列出指定上传任务已经上传的分片。
//
// imur  InitiateMultipartUpload的返回值。
//
// ListUploadedPartsResponse  操作成功后的返回值，成员UploadedParts已经上传/拷贝的片。error为nil时该返回值有效。
// error  操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) ListUploadedParts(imur InitiateMultipartUploadResult) (ListUploadedPartsResult, error) {
	var out ListUploadedPartsResult
	params := "uploadId=" + imur.UploadID
	resp, err := bucket.do("GET", imur.Key, params, params, nil, nil)
	if err != nil {
		return out, err
	}
	defer resp.body.Close()

	err = xmlUnmarshal(resp.body, &out)
	return out, err
}

//
// ListMultipartUploads 列出所有未上传完整的multipart任务列表。
//
// options  ListObject的筛选行为。Prefix返回object的前缀，KeyMarker返回object的起始位置，MaxUploads最大数目默认1000，
// Delimiter用于对Object名字进行分组的字符，所有名字包含指定的前缀且第一次出现delimiter字符之间的object。
//
// ListMultipartUploadResponse  操作成功后的返回值，error为nil时该返回值有效。
// error  操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) ListMultipartUploads(options ...Option) (ListMultipartUploadResult, error) {
	var out ListMultipartUploadResult

	options = append(options, EncodingType("url"))
	params, err := handleParams(options)
	if err != nil {
		return out, err
	}

	resp, err := bucket.do("GET", "", "uploads&"+params, "uploads", nil, nil)
	if err != nil {
		return out, err
	}
	defer resp.body.Close()

	err = xmlUnmarshal(resp.body, &out)
	if err != nil {
		return out, err
	}
	err = decodeListMultipartUploadResult(&out)
	return out, err
}

//
// UploadFile 分块上传文件，适合加大文件
//
// objectKey  object名称。
// filePath   本地文件。需要上传的文件。
// partSize   本次上传文件片的大小，字节数。比如100 * 1024为每片100KB。
// options    上传Object时可以指定Object的属性。详见InitiateMultipartUpload。
//
// error 操作成功为nil，非nil为错误信息。
//
func (bucket Bucket) UploadFile(objectKey, filePath string, partSize int64, options ...Option) error {
	if partSize < MinPartSize || partSize > MaxPartSize {
		return errors.New("oss: part size invalid range (1024KB, 5GB]")
	}

	chunks, err := SplitFileByPartSize(filePath, partSize)
	if err != nil {
		return err
	}

	imur, err := bucket.InitiateMultipartUpload(objectKey, options...)
	if err != nil {
		return err
	}

	parts := []UploadPart{}
	for _, chunk := range chunks {
		part, err := bucket.UploadPartFromFile(imur, filePath, chunk.Offset, chunk.Size,
			chunk.Number)
		if err != nil {
			bucket.AbortMultipartUpload(imur)
			return err
		}
		parts = append(parts, part)
	}

	_, err = bucket.CompleteMultipartUpload(imur, parts)
	if err != nil {
		bucket.AbortMultipartUpload(imur)
		return err
	}
	return nil
}

//
// DownloadFile 分块下载文件，适合加大Object
//
// objectKey  object key。
// filePath   本地文件。objectKey下载到文件。
// partSize   本次上传文件片的大小，字节数。比如100 * 1024为每片100KB。
// options    Object的属性限制项。详见GetObject。
//
// error 操作成功error为nil，非nil为错误信息。
//
func (bucket Bucket) DownloadFile(objectKey, filePath string, partSize int64, options ...Option) error {
	if partSize < 1 || partSize > MaxPartSize {
		return errors.New("oss: part size invalid range (1, 5GB]")
	}

	meta, err := bucket.GetObjectDetailedMeta(objectKey)
	if err != nil {
		return err
	}

	fd, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0660)
	if err != nil {
		return err
	}
	defer fd.Close()

	buf := make([]byte, bucket.getConfig().IOBufSize)
	objectSize, err := strconv.ParseInt(meta.Get(HTTPHeaderContentLength), 10, 0)
	for i := int64(0); i < objectSize; i += partSize {
		option := Range(i, GetPartEnd(i, objectSize, partSize))
		options = append(options, option)
		r, err := bucket.GetObject(objectKey, options...)
		if err != nil {
			return err
		}
		defer r.Close()
		_, err = io.CopyBuffer(fd, r, buf)
		if err != nil {
			return err
		}
	}
	return nil
}
