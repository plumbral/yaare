package files

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const shareAll = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
const accessSecurity = windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION

func privateDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;FA;;;" + user.User.Sid.String() + ")")
}

func (f Local) openCopyObject(p string, access, share, disposition, kind uint32, sd *windows.SECURITY_DESCRIPTOR) (windows.Handle, error) {
	leaf := path.Base(p)
	if !Leaf(leaf) || !filepath.IsLocal(leaf) || strings.Contains(leaf, ":") {
		return 0, fs.ErrInvalid
	}
	parent, err := f.Root.Open(localPath(path.Dir(p)))
	if err != nil {
		return 0, err
	}
	defer parent.Close()
	name, err := windows.NewNTUnicodeString(leaf)
	if err != nil {
		return 0, err
	}
	attrs := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(parent.Fd()), ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE, SecurityDescriptor: sd}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE, &attrs, &windows.IO_STATUS_BLOCK{}, nil,
		windows.FILE_ATTRIBUTE_NORMAL, share, disposition, kind|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	var status windows.NTStatus
	if errors.As(err, &status) {
		err = status.Errno()
	}
	if err != nil {
		return 0, err
	}
	return handle, nil
}

func objectACL(handle windows.Handle) (*windows.SECURITY_DESCRIPTOR, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, fs.ErrPermission
	}
	return windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, accessSecurity)
}

func aclKey(descriptor *windows.SECURITY_DESCRIPTOR) (string, bool, error) {
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return "", false, fs.ErrPermission
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return "", false, err
	}
	access, err := windows.NewSecurityDescriptor()
	if err != nil {
		return "", false, err
	}
	if err = access.SetDACL(dacl, true, false); err != nil {
		return "", false, err
	}
	key := access.String()
	runtime.KeepAlive(descriptor)
	if key == "" {
		return "", false, fs.ErrPermission
	}
	return key, control&windows.SE_DACL_PROTECTED != 0, nil
}

func sameACL(expected, actual *windows.SECURITY_DESCRIPTOR) bool {
	key, protected, err := aclKey(expected)
	other, otherProtected, otherErr := aclKey(actual)
	return err == nil && otherErr == nil && key == other && protected == otherProtected
}

func verifyPrivateObject(handle windows.Handle, expected *windows.SECURITY_DESCRIPTOR) error {
	actual, err := objectACL(handle)
	if err != nil || !sameACL(expected, actual) {
		return problem(privateStageError)
	}
	owner, _, err := actual.Owner()
	want, _, expectedErr := expected.Owner()
	if err != nil || expectedErr != nil || !owner.Equals(want) {
		return problem(privateStageError)
	}
	return nil
}

func (f Local) createPrivate(p string) (io.WriteCloser, error) {
	sd, err := privateDescriptor()
	if err != nil {
		return nil, problem(privateStageError)
	}
	handle, err := f.openCopyObject(p, windows.FILE_GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES, shareAll, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE, sd)
	if err != nil {
		return nil, err
	}
	if err = verifyPrivateObject(handle, sd); err != nil {
		_ = windows.CloseHandle(handle)
		_ = f.Remove(p, false)
		return nil, err
	}
	return os.NewFile(uintptr(handle), p), nil
}

func (f Local) openPrivateResume(p string) (ResumeHandle, error) {
	sd, err := privateDescriptor()
	if err != nil {
		return nil, problem(privateStageError)
	}
	handle, err := f.openCopyObject(p, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, shareAll, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if err != nil {
		return nil, err
	}
	if err = verifyPrivateObject(handle, sd); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	return os.NewFile(uintptr(handle), p), nil
}

func (f Local) privateDir(p string, create bool) error {
	sd, err := privateDescriptor()
	if err != nil {
		return problem(privateStageError)
	}
	disposition := uint32(windows.FILE_OPEN)
	if create {
		disposition = windows.FILE_CREATE
	}
	handle, err := f.openCopyObject(p, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, shareAll, disposition, windows.FILE_DIRECTORY_FILE, sd)
	if err != nil {
		return err
	}
	err = verifyPrivateObject(handle, sd)
	_ = windows.CloseHandle(handle)
	if err != nil && create {
		_ = f.Remove(p, true)
	}
	return err
}

func setCopyACL(handle windows.Handle, descriptor *windows.SECURITY_DESCRIPTOR) error {
	acl, _, err := descriptor.DACL()
	if err != nil || acl == nil {
		return fs.ErrPermission
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		flags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	err = windows.SetKernelObjectSecurity(handle, flags, descriptor)
	runtime.KeepAlive(descriptor)
	return err
}

func sameFilePrincipals(left, right *windows.SECURITY_DESCRIPTOR) bool {
	changes, err := principalChanges(left, right)
	return err == nil && changes == 0
}

func principalChanges(left, right *windows.SECURITY_DESCRIPTOR) (windows.SECURITY_INFORMATION, error) {
	leftOwner, _, eo := left.Owner()
	rightOwner, _, ro := right.Owner()
	leftGroup, _, eg := left.Group()
	rightGroup, _, rg := right.Group()
	if eo != nil || ro != nil || eg != nil || rg != nil || leftOwner == nil || rightOwner == nil || leftGroup == nil || rightGroup == nil {
		return 0, fs.ErrPermission
	}
	var changes windows.SECURITY_INFORMATION
	if !leftOwner.Equals(rightOwner) {
		changes |= windows.OWNER_SECURITY_INFORMATION
	}
	if !leftGroup.Equals(rightGroup) {
		changes |= windows.GROUP_SECURITY_INFORMATION
	}
	return changes, nil
}

func applyCopySecurity(handle windows.Handle, expected *windows.SECURITY_DESCRIPTOR) error {
	if _, _, err := aclKey(expected); err != nil {
		return err
	}
	current, err := objectACL(handle)
	if err != nil {
		return err
	}
	changes, err := principalChanges(current, expected)
	if err != nil {
		return err
	}
	if changes != 0 {
		if err := windows.SetKernelObjectSecurity(handle, changes, expected); err != nil {
			return err
		}
	}
	if err := setCopyACL(handle, expected); err != nil {
		return err
	}
	actual, err := objectACL(handle)
	if err != nil || !sameFilePrincipals(expected, actual) || !sameACL(expected, actual) {
		return fs.ErrPermission
	}
	return nil
}

func (f Local) inheritedCopySecurity(target string) (*windows.SECURITY_DESCRIPTOR, error) {
	probe := path.Join(path.Dir(target), ".yaare-access-"+rand.Text())
	handle, err := f.openCopyObject(probe, windows.DELETE|windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, 0,
		windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_DELETE_ON_CLOSE, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(handle)
	return objectACL(handle)
}

func setCopyAttributes(handle windows.Handle, attributes uint32) error {
	info := struct {
		times      [4]int64
		attributes uint32
		padding    uint32
	}{attributes: attributes}
	return windows.SetFileInformationByHandle(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
}

func renameCopyHandle(handle windows.Handle, parent *os.File, leaf string, replace, readonly bool) error {
	name, err := windows.UTF16FromString(leaf)
	if err != nil || !Leaf(leaf) || !filepath.IsLocal(leaf) || strings.Contains(leaf, ":") {
		return fs.ErrInvalid
	}
	name = name[:len(name)-1]
	type renameInfo struct {
		flags  uint32
		root   windows.Handle
		length uint32
		name   [1]uint16
	}
	const fileRenameInformationEx = 65
	var header renameInfo
	size := int(unsafe.Offsetof(header.name)) + 2*len(name)
	buffer := make([]byte, max(size, int(unsafe.Sizeof(header))))
	info := (*renameInfo)(unsafe.Pointer(&buffer[0]))
	if replace {
		info.flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
		if readonly {
			info.flags |= windows.FILE_RENAME_IGNORE_READONLY_ATTRIBUTE
		}
	}
	info.root = windows.Handle(parent.Fd())
	info.length = uint32(2 * len(name))
	copy(unsafe.Slice(&info.name[0], len(name)), name)
	return windows.NtSetInformationFile(handle, &windows.IO_STATUS_BLOCK{}, &buffer[0], uint32(len(buffer)), fileRenameInformationEx)
}

func (f Local) commitCopy(ctx context.Context, stage, target string, baseline FileVersion, _ fs.FileMode) error {
	if err := f.privateDir(path.Dir(stage), false); err != nil {
		return err
	}
	if err := copyDestinationUnchanged(f, target, baseline); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	src, err := f.openCopyObject(stage, windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER|windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_DELETE, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if err != nil {
		return problem(copyModeError)
	}
	defer windows.CloseHandle(src)
	private, err := privateDescriptor()
	if err != nil || verifyPrivateObject(src, private) != nil {
		return problem(privateStageError)
	}
	staged, err := objectACL(src)
	if err != nil {
		return problem(privateStageError)
	}
	var expected *windows.SECURITY_DESCRIPTOR
	if baseline.Exists {
		dst, err := f.openCopyObject(target, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_DELETE,
			windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
		if err != nil {
			return problem(copyModeError)
		}
		defer windows.CloseHandle(dst)
		expected, err = objectACL(dst)
		if err != nil {
			return problem(copyModeError)
		}
	} else {
		expected, err = f.inheritedCopySecurity(target)
		if err != nil {
			return problem(copyModeError)
		}
	}
	if _, _, err = aclKey(expected); err != nil {
		return problem(copyModeError)
	}
	parent, err := f.Root.Open(localPath(path.Dir(target)))
	if err != nil {
		return err
	}
	defer parent.Close()
	committed := false
	readonly := baseline.Exists && baseline.Mode.Perm()&0200 == 0
	defer func() {
		if !committed && (applyCopySecurity(src, staged) != nil || readonly && setCopyAttributes(src, windows.FILE_ATTRIBUTE_NORMAL) != nil) {
			flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
			_ = windows.NtSetInformationFile(src, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)), windows.FileDispositionInformationEx)
		}
	}()
	if err = applyCopySecurity(src, expected); err != nil {
		return problem(copyModeError)
	}
	if readonly && setCopyAttributes(src, windows.FILE_ATTRIBUTE_READONLY) != nil {
		return problem(copyModeError)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = renameCopyHandle(src, parent, path.Base(target), baseline.Exists, readonly); err != nil {
		return problem(copyModeError)
	}
	committed = true
	return nil
}
