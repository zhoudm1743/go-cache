package cache

import (
	"errors"
	"fmt"
	"strings"
)

// WrapError 包装错误，添加上下文信息
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

// ConvertError 将特定实现的错误转换为标准缓存错误
func ConvertError(err error) error {
	if err == nil {
		return nil
	}

	// 先检查是否已经是标准错误
	if IsStandardError(err) {
		return err
	}

	// 错误字符串匹配
	errMsg := strings.ToLower(err.Error())

	// 键不存在错误
	if strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "not exist") ||
		strings.Contains(errMsg, "no such key") ||
		strings.Contains(errMsg, "nil") {
		return ErrKeyNotFound
	}

	// 类型不匹配错误
	if strings.Contains(errMsg, "type") && strings.Contains(errMsg, "mismatch") ||
		strings.Contains(errMsg, "wrong kind") ||
		strings.Contains(errMsg, "wrong type") {
		return ErrTypeMismatch
	}

	// 键已存在错误
	if strings.Contains(errMsg, "already exists") ||
		strings.Contains(errMsg, "duplicate") {
		return ErrKeyExists
	}

	// 操作不支持错误
	if strings.Contains(errMsg, "not support") ||
		strings.Contains(errMsg, "unsupported") ||
		strings.Contains(errMsg, "not implement") {
		return ErrNotSupported
	}

	// 参数错误
	if strings.Contains(errMsg, "invalid") ||
		strings.Contains(errMsg, "argument") ||
		strings.Contains(errMsg, "parameter") {
		return ErrInvalidArgument
	}

	// 连接错误
	if strings.Contains(errMsg, "connection") ||
		strings.Contains(errMsg, "connect") ||
		strings.Contains(errMsg, "network") {
		return ErrConnectionFailed
	}

	// 超时错误
	if strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "deadline") {
		return ErrTimeout
	}

	// 缓存已满错误
	if strings.Contains(errMsg, "full") ||
		strings.Contains(errMsg, "no space") ||
		strings.Contains(errMsg, "capacity") {
		return ErrCacheFull
	}

	// 字段错误
	if strings.Contains(errMsg, "field") && strings.Contains(errMsg, "not found") {
		return ErrFieldNotFound
	}

	// 其他服务器错误
	if strings.Contains(errMsg, "server") ||
		strings.Contains(errMsg, "internal") {
		return ErrServerInternal
	}

	// 不能识别的错误，保留原样
	return err
}

// IsStandardError 判断是否是标准错误
func IsStandardError(err error) bool {
	return errors.Is(err, ErrKeyNotFound) ||
		errors.Is(err, ErrTypeMismatch) ||
		errors.Is(err, ErrKeyExists) ||
		errors.Is(err, ErrNotSupported) ||
		errors.Is(err, ErrInvalidArgument) ||
		errors.Is(err, ErrConnectionFailed) ||
		errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrCacheFull) ||
		errors.Is(err, ErrFieldNotFound) ||
		errors.Is(err, ErrServerInternal)
}

// ErrorWithContext 创建一个带上下文的标准错误
func ErrorWithContext(stdErr error, context string) error {
	if stdErr == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", context, stdErr)
}

// GetRootError 获取错误链中的根本错误
func GetRootError(err error) error {
	if err == nil {
		return nil
	}

	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}
