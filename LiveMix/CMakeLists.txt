cmake_minimum_required(VERSION 3.21)

# 版本号来源（优先级从高到低）:
#   1. -DLIVEMIX_APP_VERSION=x.y.z —— CI 从推送的 tag 提取后传入（如 v1.1.0 -> 1.1.0）
#   2. HEAD 上恰好存在的版本 tag（本地在 tag 上直接构建时）
#   3. 1.0.0
# 安装包名、Info.plist、Windows 安装器元数据和应用内显示都由此统一派生。
set(LIVEMIX_APP_VERSION "" CACHE STRING
    "应用版本号覆盖值，格式如 1.2.3（默认取 HEAD 上的版本 tag，否则 1.0.0）")

# 去掉可选的 v 前缀，校验为 1~4 段数字；无效时返回空字符串。
function(_livemix_normalize_version INPUT OUTPUT)
    string(REGEX REPLACE "^[vV]" "" _version "${INPUT}")
    if(NOT _version MATCHES "^[0-9]+(\\.[0-9]+)*$")
        set(${OUTPUT} "" PARENT_SCOPE)
        return()
    endif()
    string(REPLACE "." ";" _parts "${_version}")
    list(LENGTH _parts _count)
    if(_count GREATER 4)
        set(${OUTPUT} "" PARENT_SCOPE)
        return()
    endif()
    set(${OUTPUT} "${_version}" PARENT_SCOPE)
endfunction()

set(_livemix_version "")
set(_livemix_git_result 1)
set(_livemix_git_tag "")
if(LIVEMIX_APP_VERSION)
    _livemix_normalize_version("${LIVEMIX_APP_VERSION}" _livemix_version)
    if(NOT _livemix_version)
        message(FATAL_ERROR
            "LIVEMIX_APP_VERSION='${LIVEMIX_APP_VERSION}' 不是有效的数字版本号。"
            "请使用 1.2.3 或 1.2.3.4 这样的格式（tag 的 v 前缀可以省略）。")
    endif()
    message(STATUS "使用传入的版本号: ${_livemix_version}")
else()
    find_program(LIVEMIX_GIT_EXECUTABLE git)
    if(LIVEMIX_GIT_EXECUTABLE)
        execute_process(
            COMMAND "${LIVEMIX_GIT_EXECUTABLE}" describe --tags --exact-match
                    --match "v[0-9]*" --match "[0-9]*"
            WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
            OUTPUT_VARIABLE _livemix_git_tag
            OUTPUT_STRIP_TRAILING_WHITESPACE
            RESULT_VARIABLE _livemix_git_result
            ERROR_QUIET)
    endif()
    if(_livemix_git_result EQUAL 0)
        _livemix_normalize_version("${_livemix_git_tag}" _livemix_version)
    endif()
    if(_livemix_version)
        message(STATUS "使用 git tag 版本号: ${_livemix_version}")
    else()
        set(_livemix_version "1.0.0")
        message(STATUS "未找到可用的版本 tag，回退到版本号: ${_livemix_version}")
    endif()
endif()

project(LiveMixStudio
    VERSION ${_livemix_version}
    DESCRIPTION "LiveMix Studio desktop client"
    LANGUAGES CXX
)

# 供打包脚本读取最终解析出的版本号（Windows 安装器用它命名和填写元数据）。
file(WRITE "${CMAKE_BINARY_DIR}/livemix-version.txt" "${PROJECT_VERSION}\n")

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)

# 本地开发与 CI 打包统一使用 Qt 6.12.0，避免构建产物行为不一致。
find_package(Qt6 6.12 REQUIRED COMPONENTS Core Gui Widgets Svg Sql Network
                                        Multimedia MultimediaWidgets)

qt_standard_project_setup()

qt_add_executable(LiveMixStudio
    MACOSX_BUNDLE
    WIN32
    src/main.cpp

    src/app/ApplicationStyle.h
    src/app/ApplicationStyle.cpp
    src/app/MainWindow.h
    src/app/MainWindow.cpp

    src/core/AppConfig.h
    src/core/AppConfig.cpp
    src/core/AutoStart.h
    src/core/AutoStart.cpp
    src/core/FileManager.h
    src/core/FileManager.cpp
    src/core/Logger.h
    src/core/Logger.cpp
    src/core/MediaUtils.h
    src/core/MediaUtils.cpp
    src/core/ServiceEndpoints.h
    src/core/ServiceEndpoints.cpp

    src/api/AsrClient.h
    src/api/AsrClient.cpp
    src/api/CapCutMateClient.h
    src/api/CapCutMateClient.cpp
    src/api/HttpClient.h
    src/api/HttpClient.cpp
    src/api/LlmClient.h
    src/api/LlmClient.cpp

    src/data/ClipTypes.h
    src/data/ClipTypes.cpp
    src/data/MediaRepository.h
    src/data/MediaRepository.cpp
    src/data/MediaTypes.h
    src/data/PipelineTypes.h

    src/media/FfmpegLocator.h
    src/media/FfmpegLocator.cpp
    src/media/FfmpegUtils.h
    src/media/FfmpegUtils.cpp
    src/media/MediaProbe.h
    src/media/MediaProbe.cpp
    src/media/RecordingService.h
    src/media/RecordingService.cpp

    src/services/DraftDownloader.h
    src/services/DraftDownloader.cpp
    src/services/DraftDownloadJob.h
    src/services/DraftDownloadManager.h
    src/services/DraftDownloadManager.cpp
    src/services/LiveClipService.h
    src/services/LiveClipService.cpp
    src/services/CaptionTiming.h
    src/services/CaptionTiming.cpp

    src/model/DefaultPrompts.h
    src/model/GarmentPrompts.h
    src/model/DemoData.h

    src/ui/Icons.h
    src/ui/Icons.cpp
    src/ui/Controls.h
    src/ui/Controls.cpp
    src/ui/DraftDownloadDialog.h
    src/ui/DraftDownloadDialog.cpp
    src/ui/WindowChrome.h
    src/ui/WindowChrome.cpp
    src/ui/Overlays.h
    src/ui/Overlays.cpp
    src/ui/VideoPreview.h
    src/ui/VideoPreview.cpp

    src/pages/DashboardPage.h
    src/pages/DashboardPage.cpp
    src/pages/MaterialPage.h
    src/pages/MaterialPage.cpp
    src/pages/ResultsPage.h
    src/pages/ResultsPage.cpp
    src/pages/PipelinePage.h
    src/pages/PipelinePage.cpp
    src/pages/PromptPage.h
    src/pages/PromptPage.cpp
    src/pages/TasksPage.h
    src/pages/TasksPage.cpp
    src/pages/SettingsPage.h
    src/pages/SettingsPage.cpp
    src/pages/SpecPage.h
    src/pages/SpecPage.cpp

    resources/LiveMixStudio.qrc
)

target_link_libraries(LiveMixStudio PRIVATE
    Qt6::Core Qt6::Gui Qt6::Widgets Qt6::Svg Qt6::Sql Qt6::Network
    Qt6::Multimedia Qt6::MultimediaWidgets)

target_compile_definitions(LiveMixStudio PRIVATE
    LIVEMIX_VERSION="${PROJECT_VERSION}"
)

if(MSVC)
    target_compile_options(LiveMixStudio PRIVATE /utf-8 /permissive-)
else()
    target_compile_options(LiveMixStudio PRIVATE -Wall -Wextra -Wpedantic)
endif()

set_target_properties(LiveMixStudio PROPERTIES
    MACOSX_BUNDLE_BUNDLE_NAME "LiveMix Studio"
    MACOSX_BUNDLE_GUI_IDENTIFIER "com.livemix.studio"
    MACOSX_BUNDLE_BUNDLE_VERSION "${PROJECT_VERSION}"
    MACOSX_BUNDLE_SHORT_VERSION_STRING "${PROJECT_VERSION}"
    WIN32_EXECUTABLE TRUE
)

if(APPLE)
    target_compile_definitions(LiveMixStudio PRIVATE LIVEMIX_PLATFORM_MACOS)
    # Bundle the brand icon so Finder/Dock show the LiveMix logo.
    set_source_files_properties(resources/AppIcon.icns PROPERTIES
        MACOSX_PACKAGE_LOCATION "Resources")
    target_sources(LiveMixStudio PRIVATE resources/AppIcon.icns)
    set_target_properties(LiveMixStudio PROPERTIES
        MACOSX_BUNDLE_ICON_FILE "AppIcon.icns")
elseif(WIN32)
    target_compile_definitions(LiveMixStudio PRIVATE LIVEMIX_PLATFORM_WINDOWS)
    # Executable/installer icon; the runtime QIcon comes from the Qt resource.
    target_include_directories(LiveMixStudio PRIVATE resources)
    target_sources(LiveMixStudio PRIVATE resources/AppIcon.rc)
endif()

# Bundle ffmpeg/ffprobe so installers are self-contained. The binaries are
# fetched at build time by scripts/fetch-ffmpeg.* and are never committed.
set(LIVEMIX_FFMPEG_DIR "$ENV{LIVEMIX_FFMPEG_DIR}" CACHE PATH
    "Directory containing the ffmpeg and ffprobe binaries to bundle")

if(LIVEMIX_FFMPEG_DIR)
    if(WIN32)
        set(_livemix_ffmpeg_names ffmpeg.exe ffprobe.exe)
    else()
        set(_livemix_ffmpeg_names ffmpeg ffprobe)
    endif()

    set(_livemix_ffmpeg_files)
    foreach(_name IN LISTS _livemix_ffmpeg_names)
        set(_path "${LIVEMIX_FFMPEG_DIR}/${_name}")
        if(NOT EXISTS "${_path}")
            message(FATAL_ERROR
                "LIVEMIX_FFMPEG_DIR is missing ${_name}: ${_path}\n"
                "Run scripts/fetch-ffmpeg.sh (macOS) or scripts/fetch-ffmpeg.ps1 (Windows) first.")
        endif()
        list(APPEND _livemix_ffmpeg_files "${_path}")
    endforeach()

    foreach(_extra IN ITEMS ffmpeg-LICENSE.txt ffmpeg-BUILD-README.txt ffmpeg-SOURCE.txt)
        if(EXISTS "${LIVEMIX_FFMPEG_DIR}/${_extra}")
            list(APPEND _livemix_ffmpeg_files "${LIVEMIX_FFMPEG_DIR}/${_extra}")
        endif()
    endforeach()

    if(APPLE)
        set(_livemix_ffmpeg_dest "$<TARGET_BUNDLE_DIR:LiveMixStudio>/Contents/Helpers")
    else()
        set(_livemix_ffmpeg_dest "$<TARGET_FILE_DIR:LiveMixStudio>")
    endif()

    add_custom_command(TARGET LiveMixStudio POST_BUILD
        COMMAND ${CMAKE_COMMAND} -E make_directory "${_livemix_ffmpeg_dest}"
        COMMAND ${CMAKE_COMMAND} -E copy_if_different
                ${_livemix_ffmpeg_files} "${_livemix_ffmpeg_dest}"
        COMMENT "Bundling ffmpeg binaries into the application"
        VERBATIM)

    message(STATUS "Bundling ffmpeg binaries from ${LIVEMIX_FFMPEG_DIR}")
endif()

include(GNUInstallDirs)
install(TARGETS LiveMixStudio
    BUNDLE DESTINATION .
    RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
)
