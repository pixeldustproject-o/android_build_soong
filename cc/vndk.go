// Copyright 2017 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cc

import (
	"sort"
	"strings"
	"sync"

	"android/soong/android"
)

type VndkProperties struct {
	Vndk struct {
		// declared as a VNDK or VNDK-SP module. The vendor variant
		// will be installed in /system instead of /vendor partition.
		//
		// `vendor_vailable` must be explicitly set to either true or
		// false together with `vndk: {enabled: true}`.
		Enabled *bool

		// declared as a VNDK-SP module, which is a subset of VNDK.
		//
		// `vndk: { enabled: true }` must set together.
		//
		// All these modules are allowed to link to VNDK-SP or LL-NDK
		// modules only. Other dependency will cause link-type errors.
		//
		// If `support_system_process` is not set or set to false,
		// the module is VNDK-core and can link to other VNDK-core,
		// VNDK-SP or LL-NDK modules only.
		Support_system_process *bool
	}
}

type vndkdep struct {
	Properties VndkProperties
}

func (vndk *vndkdep) props() []interface{} {
	return []interface{}{&vndk.Properties}
}

func (vndk *vndkdep) begin(ctx BaseModuleContext) {}

func (vndk *vndkdep) deps(ctx BaseModuleContext, deps Deps) Deps {
	return deps
}

func (vndk *vndkdep) isVndk() bool {
	return Bool(vndk.Properties.Vndk.Enabled)
}

func (vndk *vndkdep) isVndkSp() bool {
	return Bool(vndk.Properties.Vndk.Support_system_process)
}

func (vndk *vndkdep) typeName() string {
	if !vndk.isVndk() {
		return "native:vendor"
	}
	if !vndk.isVndkSp() {
		return "native:vendor:vndk"
	}
	return "native:vendor:vndksp"
}

func (vndk *vndkdep) vndkCheckLinkType(ctx android.ModuleContext, to *Module) {
	if to.linker == nil {
		return
	}
	if !vndk.isVndk() {
		// Non-VNDK modules (those installed to /vendor) can't depend on modules marked with
		// vendor_available: false.
		violation := false
		if lib, ok := to.linker.(*llndkStubDecorator); ok && !lib.Properties.Vendor_available {
			violation = true
		} else {
			if _, ok := to.linker.(libraryInterface); ok && to.VendorProperties.Vendor_available != nil && !Bool(to.VendorProperties.Vendor_available) {
				// Vendor_available == nil && !Bool(Vendor_available) should be okay since
				// it means a vendor-only library which is a valid dependency for non-VNDK
				// modules.
				violation = true
			}
		}
		if violation {
			ctx.ModuleErrorf("Vendor module that is not VNDK should not link to %q which is marked as `vendor_available: false`", to.Name())
		}
	}
	if lib, ok := to.linker.(*libraryDecorator); !ok || !lib.shared() {
		// Check only shared libraries.
		// Other (static and LL-NDK) libraries are allowed to link.
		return
	}
	if !to.Properties.UseVndk {
		ctx.ModuleErrorf("(%s) should not link to %q which is not a vendor-available library",
			vndk.typeName(), to.Name())
		return
	}
	if to.vndkdep == nil {
		return
	}
	if (vndk.isVndk() && !to.vndkdep.isVndk()) || (vndk.isVndkSp() && !to.vndkdep.isVndkSp()) {
		ctx.ModuleErrorf("(%s) should not link to %q(%s)",
			vndk.typeName(), to.Name(), to.vndkdep.typeName())
		return
	}
}

var (
	vndkCoreLibraries    []string
	vndkSpLibraries      []string
	llndkLibraries       []string
	vndkPrivateLibraries []string
	vndkLibrariesLock    sync.Mutex
)

// gather list of vndk-core, vndk-sp, and ll-ndk libs
func vndkMutator(mctx android.BottomUpMutatorContext) {
	if m, ok := mctx.Module().(*Module); ok {
		if lib, ok := m.linker.(*llndkStubDecorator); ok {
			vndkLibrariesLock.Lock()
			defer vndkLibrariesLock.Unlock()
			name := strings.TrimSuffix(m.Name(), llndkLibrarySuffix)
			if !inList(name, llndkLibraries) {
				llndkLibraries = append(llndkLibraries, name)
				sort.Strings(llndkLibraries)
			}
			if !lib.Properties.Vendor_available {
				if !inList(name, vndkPrivateLibraries) {
					vndkPrivateLibraries = append(vndkPrivateLibraries, name)
					sort.Strings(vndkPrivateLibraries)
				}
			}
		} else {
			lib, is_lib := m.linker.(*libraryDecorator)
			prebuilt_lib, is_prebuilt_lib := m.linker.(*prebuiltLibraryLinker)
			if (is_lib && lib.shared()) || (is_prebuilt_lib && prebuilt_lib.shared()) {
				name := strings.TrimPrefix(m.Name(), "prebuilt_")
				if m.vndkdep.isVndk() {
					vndkLibrariesLock.Lock()
					defer vndkLibrariesLock.Unlock()
					if m.vndkdep.isVndkSp() {
						if !inList(name, vndkSpLibraries) {
							vndkSpLibraries = append(vndkSpLibraries, name)
							sort.Strings(vndkSpLibraries)
						}
					} else {
						if !inList(name, vndkCoreLibraries) {
							vndkCoreLibraries = append(vndkCoreLibraries, name)
							sort.Strings(vndkCoreLibraries)
						}
					}
					if !Bool(m.VendorProperties.Vendor_available) {
						if !inList(name, vndkPrivateLibraries) {
							vndkPrivateLibraries = append(vndkPrivateLibraries, name)
							sort.Strings(vndkPrivateLibraries)
						}
					}
				}
			}
		}

	}
}
