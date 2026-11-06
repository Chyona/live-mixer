cmake_minimum_required(VERSION 3.21)

project(LiveMixStudio
    VERSION 1.0.0
    DESCRIPTION "LiveMix Studio desktop client"
    LANGUAGES CXX
)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)

find_package(Qt6 6.5 REQUIRED COMPONENTS Core Gui Widgets Svg Sql Network
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
