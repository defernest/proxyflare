from unittest.mock import MagicMock

import pytest

from proxyflare.validation import WORKER_PERMISSIONS, check_token_permissions, verify_token


def test_verify_token_success():
    mock_resource = MagicMock()
    mock_response = MagicMock()
    mock_response.id = "token_id"
    mock_response.status = "active"
    mock_resource.verify.return_value = mock_response

    token_id = verify_token(mock_resource)
    assert token_id == "token_id"  # noqa: S105


def test_verify_token_failed_none():
    mock_resource = MagicMock()
    mock_resource.verify.return_value = None

    with pytest.raises(ValueError, match="Token verification failed"):
        verify_token(mock_resource)


def test_verify_token_inactive():
    mock_resource = MagicMock()
    mock_response = MagicMock()
    mock_response.status = "disabled"
    mock_resource.verify.return_value = mock_response

    with pytest.raises(ValueError, match="Token is not active"):
        verify_token(mock_resource)


def test_check_permissions_success():
    mock_resource = MagicMock()
    mock_response = MagicMock()

    # Create a policy with all permissions
    mock_policy = MagicMock()
    mock_policy.effect = "allow"
    mock_policy.permission_groups = [MagicMock(name=p) for p in WORKER_PERMISSIONS]
    # Configure names
    for pg, name in zip(mock_policy.permission_groups, WORKER_PERMISSIONS, strict=True):
        pg.name = name

    mock_response.policies = [mock_policy]
    mock_resource.get.return_value = mock_response

    check_token_permissions(mock_resource, "token_id")


def test_check_permissions_missing():
    mock_resource = MagicMock()
    mock_response = MagicMock()

    mock_policy = MagicMock()
    mock_policy.effect = "allow"
    # Provide only one permission
    pg = MagicMock()
    pg.name = WORKER_PERMISSIONS[0]
    mock_policy.permission_groups = [pg]

    mock_response.policies = [mock_policy]
    mock_resource.get.return_value = mock_response

    with pytest.raises(ValueError, match="Missing required permissions"):
        check_token_permissions(mock_resource, "token_id")
