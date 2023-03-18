# coding: utf-8

"""
    lakeFS API

    lakeFS HTTP API  # noqa: E501

    The version of the OpenAPI document: 0.1.0
    Contact: services@treeverse.io
    Generated by: https://openapi-generator.tech
"""

from lakefs_client.paths.config_garbage_collection.get import GetGarbageCollectionConfig
from lakefs_client.paths.config_version.get import GetLakeFsVersion
from lakefs_client.paths.setup_lakefs.get import GetSetupState
from lakefs_client.paths.config_storage.get import GetStorageConfig
from lakefs_client.paths.setup_lakefs.post import Setup
from lakefs_client.paths.setup_comm_prefs.post import SetupCommPrefs


class ConfigApi(
    GetGarbageCollectionConfig,
    GetLakeFsVersion,
    GetSetupState,
    GetStorageConfig,
    Setup,
    SetupCommPrefs,
):
    """NOTE: This class is auto generated by OpenAPI Generator
    Ref: https://openapi-generator.tech

    Do not edit the class manually.
    """
    pass